package main

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/delucks/go-subsonic"
	"github.com/logank/itunes2subsonic/internal/report"
)

const (
	playlistRetryAttempts = 3
)

var retrySleep = time.Sleep

var errMissingPlaylistID = errors.New("missing playlistId")

type httpStatusError struct {
	StatusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http status %d", e.StatusCode)
}

type subsonicAPIError struct {
	Code    int
	Message string
}

func (e *subsonicAPIError) Error() string {
	return fmt.Sprintf("Error #%d: %s", e.Code, e.Message)
}

type playlistFailure struct {
	Name         string
	IntendedOp   string
	Adds         int
	Removes      int
	BatchSize    int
	Category     string
	ErrorMessage string
}

func writePlaylistFailuresTSV(path string, rows []playlistFailure) error {
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		data = append(data, []string{
			row.Name,
			row.IntendedOp,
			strconv.Itoa(row.Adds),
			strconv.Itoa(row.Removes),
			strconv.Itoa(row.BatchSize),
			row.Category,
			row.ErrorMessage,
		})
	}
	return report.WriteTSV(path, []string{"name", "intended_op", "adds", "removes", "batch_size", "error_category", "error_message"}, data)
}

func batchIDs(ids []string, size int) [][]string {
	return chunkStrings(ids, size)
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *subsonicAPIError
	if errors.As(err, &apiErr) {
		return false
	}

	if errors.Is(err, io.EOF) {
		return true
	}

	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusBadGateway || statusErr.StatusCode == http.StatusServiceUnavailable || statusErr.StatusCode == http.StatusGatewayTimeout
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") || strings.Contains(message, "unexpected eof")
}

func withRetry(maxAttempts int, baseDelay time.Duration, op func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxAttempts || !isTransientError(err) {
			return err
		}
		if baseDelay > 0 {
			backoff := baseDelay * time.Duration(1<<(attempt-1))
			jitterMax := int64(baseDelay / 2)
			if jitterMax > 0 {
				backoff += time.Duration(rand.Int63n(jitterMax))
			}
			retrySleep(backoff)
		}
	}
	return lastErr
}

func updatePlaylistBatched(request func(endpoint string, params url.Values) error, playlistID string, ids []string, batchSize int) error {
	if strings.TrimSpace(playlistID) == "" {
		return errMissingPlaylistID
	}
	for _, chunk := range batchIDs(ids, batchSize) {
		params := buildUpdatePlaylistParams(playlistID, chunk, nil)
		if err := request("updatePlaylist", params); err != nil {
			return err
		}
	}
	return nil
}

// Navidrome requires deletePlaylist to receive `id`, while updatePlaylist uses `playlistId`.
func buildDeletePlaylistParams(playlistID string) url.Values {
	params := url.Values{}
	params.Add("id", playlistID)
	return params
}

// Navidrome updatePlaylist requires `playlistId` plus one or more songIdToAdd/songIdToRemove values.
func buildUpdatePlaylistParams(playlistID string, songIDsToAdd []string, songIDsToRemove []string) url.Values {
	params := url.Values{}
	params.Add("playlistId", playlistID)
	for _, id := range songIDsToAdd {
		params.Add("songIdToAdd", id)
	}
	for _, id := range songIDsToRemove {
		params.Add("songIdToRemove", id)
	}
	return params
}

func playlistQueryKeys(params url.Values) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func logPlaylistRequest(endpoint string, playlistName string, playlistID string, params url.Values) {
	fmt.Fprintf(stderrWriter, "Playlist API request endpoint=%s playlist=%q playlistID=%q queryKeys=%v\n", endpoint, playlistName, playlistID, playlistQueryKeys(params))
}

func findPlaylistIDByName(playlists []*subsonic.Playlist, name string) (string, int) {
	ids := make([]string, 0)
	for _, playlist := range playlists {
		if playlist.Name == name && strings.TrimSpace(playlist.ID) != "" {
			ids = append(ids, playlist.ID)
		}
	}
	if len(ids) == 0 {
		return "", 0
	}
	// Prefer exact-name matches; for duplicates pick the first returned ID for deterministic behavior.
	return ids[0], len(ids)
}

func ensurePlaylistID(c *subsonic.Client, name string) (string, error) {
	var initial []*subsonic.Playlist
	if err := withRetry(playlistRetryAttempts, 200*time.Millisecond, func() error {
		playlists, err := c.GetPlaylists(nil)
		if err != nil {
			return err
		}
		initial = playlists
		return nil
	}); err != nil {
		return "", fmt.Errorf("lookup playlists before create: %w", err)
	}
	if id, dupes := findPlaylistIDByName(initial, name); id != "" {
		if dupes > 1 {
			fmt.Fprintf(stderrWriter, "Warning: multiple playlists named %q found (%d); using playlistId=%s\n", name, dupes, id)
		}
		return id, nil
	}

	if err := withRetry(playlistRetryAttempts, 200*time.Millisecond, func() error {
		createParams := url.Values{}
		createParams.Add("name", name)
		logPlaylistRequest("createPlaylist", name, "", createParams)
		return subsonicRequest(c, "createPlaylist", createParams)
	}); err != nil {
		return "", fmt.Errorf("create playlist: %w", err)
	}

	var afterCreate []*subsonic.Playlist
	if err := withRetry(playlistRetryAttempts, 200*time.Millisecond, func() error {
		playlists, err := c.GetPlaylists(nil)
		if err != nil {
			return err
		}
		afterCreate = playlists
		return nil
	}); err != nil {
		return "", fmt.Errorf("lookup playlists after create: %w", err)
	}
	if id, dupes := findPlaylistIDByName(afterCreate, name); id != "" {
		if dupes > 1 {
			fmt.Fprintf(stderrWriter, "Warning: multiple playlists named %q found after create (%d); using playlistId=%s\n", name, dupes, id)
		}
		return id, nil
	}
	return "", errors.New("playlist_not_found")
}

func categorizePlaylistError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errMissingPlaylistID) {
		return "missing_playlist_id"
	}
	if strings.Contains(err.Error(), "playlist_not_found") {
		return "playlist_not_found"
	}
	var apiErr *subsonicAPIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("subsonic_error_%d", apiErr.Code)
	}
	if isTransientError(err) {
		return "transient"
	}
	return "non_transient"
}
