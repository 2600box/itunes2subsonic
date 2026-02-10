package main

// Notes:
// -   Normalizes paths to lower case because Apple Music/Windows doesn't update if the underlying file changes.
// -   Navidrome requires going into the Player settings and configuring "Report Real Path"

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/delucks/go-subsonic"
	i2s "github.com/logank/itunes2subsonic"
	"github.com/logank/itunes2subsonic/internal/itunes"
	"github.com/logank/itunes2subsonic/internal/report"
	pkgreport "github.com/logank/itunes2subsonic/pkg/report"
	pb "github.com/schollz/progressbar/v3"
	"golang.org/x/term"
	"golang.org/x/text/unicode/norm"
)

var (
	dryRun                  = flag.Bool("dry_run", true, "don't modify the library")
	itunesXml               = flag.String("itunes_xml", "Apple Music Library.xml", "path to the Apple Music Library XML to import")
	skipCount               = flag.Int("skip_count", 10, "a limit on the number of tracks that would be skipped before refusing to process")
	copyUnrated             = flag.Bool("copy_unrated", false, "if true, will unset rating if src is unrated")
	subsonicUrl             = flag.String("subsonic", "", "url of the Navidrome instance")
	updatePlay              = flag.Bool("update_played", true, "update play count and last played time")
	syncStarred             = flag.Bool("sync_starred", true, "sync Apple Music favourited/loved tracks to Navidrome starred")
	syncPlaylist            = flag.Bool("sync_playlists", true, "sync Apple Music playlists to Navidrome")
	playlistBatchSize       = flag.Int("playlist_batch_size", 250, "number of songs per updatePlaylist request when syncing playlists")
	maxScrobbles            = flag.Int("max_scrobbles", 250, "maximum scrobbles per track when syncing play counts")
	createdFile             = flag.String("created_file", "", "a file to write SQL statements to update the created time")
	itunesRoot              = flag.String("itunes_root", "", "(optional) library prefix for Apple Music content")
	subsonicRoot            = flag.String("subsonic_root", "", "(optional) library prefix for Navidrome content")
	musicRoot               = flag.String("music_root", "", "(optional) root of the on-disk music folder for real-path checks")
	extensionsFlag          = flag.String("extensions", "", "comma-separated list of file extensions to match (default: common audio types)")
	verifySrcFiles          = flag.Bool("verify_src_files", false, "verify iTunes file paths exist on disk and classify stale entries")
	filterAlbum             = flag.String("filter_album", "", "only sync tracks whose album contains this text")
	filterArtist            = flag.String("filter_artist", "", "only sync tracks whose artist contains this text")
	filterName              = flag.String("filter_name", "", "only sync tracks whose title contains this text")
	filterPath              = flag.String("filter_path", "", "only sync tracks whose path contains this text")
	limitTracks             = flag.Int("limit_tracks", 0, "only sync the first N matching tracks (0 means no limit)")
	debugMode               = flag.Bool("debug", false, "enable debug logging for filtering and matching")
	logFile                 = flag.String("log_file", "", "write logs to the specified file (defaults to stderr only)")
	dumpFile                = flag.String("navidrome_dump", "", "write Navidrome track metadata (including raw paths) to a JSON file")
	writeMissing            = flag.String("write_missing", "", "write missing track metadata to a JSON file")
	analyseDump             = flag.String("analyse_dump", "", "analyse a Navidrome dump JSON file and print a summary")
	analyseReport           = flag.String("analyse_report", "", "write analysis output from --analyse_dump to JSON")
	analyseMissing          = flag.String("analyse_missing", "", "include an existing missing report when analysing a dump")
	reportLibrary           = flag.String("report_library_stats", "", "write Library.xml stats report to JSON (empty disables)")
	reportSyncPlan          = flag.String("report_sync_plan", "", "write sync plan report to JSON (empty disables)")
	reportSyncPlanTSV       = flag.String("report_sync_plan_tsv", "", "write TSV sync plan reports using the given path as a base name (empty disables)")
	reportReconcile         = flag.String("report_reconcile", "", "write reconcile report to JSON (empty disables)")
	reportOutTSV            = flag.String("out_tsv", "", "write a TSV summary when reporting library stats")
	reportRemoteMatchJSON   = flag.String("report_remote_match_json", "", "write remote loved/rated match report to JSON (empty disables)")
	reportRemoteMatchTSV    = flag.String("report_remote_match_tsv", "", "write remote loved/rated match report to TSV (empty disables)")
	reportRemoteActionable  = flag.String("report_remote_actionable_tsv", "", "write actionable remote loved/rated matches to TSV (empty disables)")
	reportRemoteStreaming   = flag.String("report_remote_streaming_gaps", "", "write remote streaming gaps report to the provided directory (empty disables)")
	remoteActionableInclude = flag.Bool("remote_actionable_include_low_confidence", false, "include LOW_CONFIDENCE matches in actionable remote report")
	reportOnly              = flag.Bool("report_only", false, "avoid fetching the full Navidrome song list when filters are active (requires --navidrome_dump)")
	subsonicClient          = flag.String("subsonic_client", "itunes2subsonic", "Subsonic client identifier (c=) to use when connecting")
	requireRealPath         = flag.Bool("require_real_path", true, "fail fast if Navidrome returns virtual/tag paths instead of real paths")
	matchMode               = flag.String("match_mode", "realpath", "path matching mode: realpath or lenient")
	probeSongID             = flag.String("probe_song_id", "", "if set, fetch /rest/getSong for the given ID and validate its path")
	probePath               = flag.String("probe_path", "", "if set, normalise the path and check for matches in the Navidrome dump/index")
	allowUnstar             = flag.Bool("allow_unstar", false, "allow unstar operations when --dry_run=false")
	syncUnstar              = flag.Bool("sync_unstar", false, "allow planning unstar operations (default: off)")
	allowReconcileMismatch  = flag.Bool("allow_reconcile_mismatch", false, "allow reconcile invariant mismatches (writes report, exits 0)")
	configFile              = flag.String("config", "", "path to a YAML config file containing presets")
	presetName              = flag.String("preset", "", "preset name to load from --config")
	dumpPreset              = flag.Bool("dump_preset", false, "print the resolved preset configuration and exit")
	runDir                  = flag.String("run_dir", "", "directory for audit/report outputs (default: run/<timestamp>)")
	auditFlag               = flag.Bool("audit", false, "run the standard audit workflow")
	force                   = flag.Bool("force", false, "overwrite existing files in --run_dir")
	failOnUnappliedLoved    = flag.Bool("fail_on_unapplied_loved", false, "exit non-zero when Loved not applied count is greater than zero")
	remoteMatchTopN         = flag.Int("remote_match_topn", 5, "maximum number of candidates to include in remote match reports")
	remoteMatchThreshold    = flag.Float64("remote_match_threshold", 0.87, "threshold for MATCH status in remote match report")
	remoteMatchLowThreshold = flag.Float64("remote_match_low_threshold", 0.75, "threshold for LOW_CONFIDENCE status in remote match report")
	remoteMatchDebug        = flag.Bool("remote_match_debug", false, "print debug information for remote match mismatches")
	verifyFlag              = flag.Bool("verify", false, "run the verify workflow (audit + verify_src_files) and emit a readiness report")
	applyFlag               = flag.Bool("apply", false, "apply changes (alias for --dry_run=false, overrides preset dry_run)")
	maxStaleMissingStars    = flag.Int("max_stale_missing_on_disk_stars", 0, "max stale_missing_on_disk allowed for stars before NO-GO")
	maxStaleMissingRatings  = flag.Int("max_stale_missing_on_disk_ratings", 0, "max stale_missing_on_disk allowed for ratings before NO-GO")
	maxStaleMissingPlays    = flag.Int("max_stale_missing_on_disk_playcounts", 0, "max stale_missing_on_disk allowed for playcounts before NO-GO")
	invalidLocationFatal    = flag.Bool("invalid_location_fatal", false, "treat invalid_location for stars/ratings/playcounts as a NO-GO condition")
	playlistInvalidFatal    = flag.Bool("playlist_invalid_location_fatal", false, "treat playlist invalid_location as a NO-GO condition")
	pathResolveOnDisk       = flag.Bool("path_resolve_on_disk", true, "attempt case/unicode path resolution when verifying source files")
	explainNotApplied       = flag.Bool("explain_not_applied", false, "print example rows for not-applied reasons from a run directory")
	explainNotAppliedTopN   = flag.Int("explain_not_applied_topn", 3, "number of examples to print per not-applied reason")
)

var (
	stdoutWriter io.Writer = os.Stdout
	stderrWriter io.Writer = os.Stderr
)

type subsonicInfo struct {
	id              string
	path            string
	rating          int
	playCount       int64
	starred         bool
	title           string
	artist          string
	album           string
	trackNumber     int
	discNumber      int
	durationSeconds int
	year            int
}

func (s subsonicInfo) Id() string          { return s.id }
func (s subsonicInfo) Path() string        { return s.path }
func (s subsonicInfo) FiveStarRating() int { return s.rating }

type itunesInfo struct {
	id        int
	path      string
	name      string
	artist    string
	album     string
	rating    int
	playDate  time.Time
	dateAdded time.Time
	playCount int
	loved     bool
	favorited bool
	hasLoved  bool
	hasFav    bool
}

func (s itunesInfo) Id() string          { return strconv.Itoa(s.id) }
func (s itunesInfo) Path() string        { return s.path }
func (s itunesInfo) FiveStarRating() int { return s.rating / 20 }
func (s itunesInfo) IsFavourite() bool {
	if s.hasFav && s.favorited {
		return true
	}
	if s.hasLoved && s.loved {
		return true
	}
	if s.hasFav {
		return s.favorited
	}
	if s.hasLoved {
		return s.loved
	}
	return false
}

type songPair struct {
	src itunesInfo
	dst subsonicInfo
}

type unstarCandidate struct {
	navidrome subsonicInfo
	apple     itunesInfo
	reason    string
}

type playlistRef struct {
	Name   string
	Master bool
	Items  []itunes.PlaylistItem
}

type missingStats struct {
	SrcTotal                  int `json:"src_total"`
	SrcEligible               int `json:"src_eligible"`
	SrcRemoteCount            int `json:"src_remote_count"`
	SrcInvalidLocationCount   int `json:"src_invalid_location_count"`
	SrcExcludedExtensionCount int `json:"src_excluded_extension_count"`
	SrcStaleMissingCount      int `json:"src_stale_missing_count"`
	SrcPathMismatchCount      int `json:"src_path_mismatch_count"`
	DstTotal                  int `json:"dst_total"`
	DstEligible               int `json:"dst_eligible"`
}

type missingCounts struct {
	MissingSrcCount       int `json:"missing_src_count"`
	MissingDstCount       int `json:"missing_dst_count"`
	PresentOnlyInDstCount int `json:"present_only_in_dst_count"`
}

type missingSong struct {
	ID           string `json:"id,omitempty"`
	Path         string `json:"path,omitempty"`
	ResolvedPath string `json:"resolved_path,omitempty"`
	Name         string `json:"name,omitempty"`
	Artist       string `json:"artist,omitempty"`
	Album        string `json:"album,omitempty"`
	RawPath      string `json:"raw_path,omitempty"`
	DecodedPath  string `json:"decoded_path,omitempty"`
	CleanPath    string `json:"clean_path,omitempty"`
	Extension    string `json:"extension,omitempty"`
}

type missingEntry struct {
	Side     string       `json:"side"`
	Reason   string       `json:"reason"`
	MatchKey string       `json:"match_key"`
	Src      *missingSong `json:"src,omitempty"`
	Dst      *missingSong `json:"dst,omitempty"`
}

type missingReport struct {
	GeneratedAt                string         `json:"generated_at"`
	Version                    string         `json:"version,omitempty"`
	MatchMode                  string         `json:"match_mode"`
	RequireRealPath            bool           `json:"require_real_path"`
	SubsonicClient             string         `json:"subsonic_client"`
	MusicRoot                  string         `json:"music_root"`
	Extensions                 []string       `json:"extensions"`
	Stats                      missingStats   `json:"stats"`
	Counts                     missingCounts  `json:"counts"`
	Missing                    []missingEntry `json:"missing"`
	SrcRemoteSamples           []missingSong  `json:"src_remote_samples"`
	SrcInvalidLocationSamples  []missingSong  `json:"src_invalid_location_samples"`
	ExcludedExtensionSamples   []missingSong  `json:"excluded_extension_samples"`
	StaleSrcFileSamples        []missingSong  `json:"stale_src_file_samples"`
	SrcPathMismatchSamples     []missingSong  `json:"src_path_mismatch_samples"`
	NotInNavidromeIndexSamples []missingEntry `json:"not_in_navidrome_index_samples"`
	NotInNavidromeIndexByExt   map[string]int `json:"not_in_navidrome_index_by_ext"`
	NotInNavidromeIndexByDir   map[string]int `json:"not_in_navidrome_index_by_dir_prefix"`
}

type analyseSummary struct {
	Total                       int                              `json:"total"`
	AbsoluteCount               int                              `json:"absolute_count"`
	RelativeCount               int                              `json:"relative_count"`
	ExtensionCounts             map[string]int                   `json:"extension_counts"`
	TopDirectories              map[string]int                   `json:"top_directories"`
	MissingReasonCounts         map[string]int                   `json:"missing_reason_counts,omitempty"`
	MissingExamples             map[string][]missingEntrySummary `json:"missing_examples,omitempty"`
	NotInNavidromeIndexByExt    map[string]int                   `json:"not_in_navidrome_index_by_ext,omitempty"`
	NotInNavidromeIndexByDir    map[string]int                   `json:"not_in_navidrome_index_by_dir_prefix,omitempty"`
	NotInNavidromeIndexExamples []missingEntrySummary            `json:"not_in_navidrome_index_examples,omitempty"`
}

type missingEntrySummary struct {
	Side     string `json:"side"`
	Reason   string `json:"reason"`
	MatchKey string `json:"match_key"`
	Path     string `json:"path,omitempty"`
}

type missingAnalysis struct {
	ReasonCounts             map[string]int
	ReasonExamples           map[string][]missingEntrySummary
	NotInNavidromeIndexByExt map[string]int
	NotInNavidromeIndexByDir map[string]int
	NotInNavidromeExamples   []missingEntrySummary
}

type dumpAnalysisReport struct {
	GeneratedAt string         `json:"generated_at"`
	DumpPath    string         `json:"dump_path"`
	Summary     analyseSummary `json:"summary"`
}

type appConfig struct {
	SubsonicURL  string `json:"subsonic_url"`
	SubsonicUser string `json:"subsonic_user"`
	SubsonicPass string `json:"subsonic_pass"`
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "itunes2subsonic", "config.json"), nil
}

func loadConfig() appConfig {
	path, err := configPath()
	if err != nil {
		return appConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return appConfig{}
	}
	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return appConfig{}
	}
	return cfg
}

func saveConfig(cfg appConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func promptInput(reader *bufio.Reader, label string) (string, error) {
	fmt.Fprintf(stdoutWriter, "%s: ", label)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func promptPassword(label string) (string, error) {
	fmt.Fprintf(stdoutWriter, "%s: ", label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(stdoutWriter, "")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(pass)), nil
	}
	reader := bufio.NewReader(os.Stdin)
	return promptInput(reader, label)
}

func fetchSubsonicSongs(c *subsonic.Client, bar *pb.ProgressBar) ([]subsonicInfo, error) {
	var tracks []subsonicInfo

	offset := 0
	for {
		songs, err := c.Search3(`""`, map[string]string{
			"songCount":   "400",
			"songOffset":  strconv.Itoa(offset),
			"artistCount": "0",
			"albumCount":  "0",
		})
		if err != nil {
			log.Fatalf("Failed fetching Navidrome songs: %s", err)
		}

		for _, s := range songs.Song {
			tracks = append(tracks, subsonicInfo{
				id:              s.ID,
				path:            s.Path,
				rating:          s.UserRating,
				playCount:       s.PlayCount,
				starred:         false,
				title:           s.Title,
				artist:          s.Artist,
				album:           s.Album,
				trackNumber:     s.Track,
				discNumber:      s.DiscNumber,
				durationSeconds: s.Duration,
				year:            s.Year,
			})
		}

		if len(songs.Song) == 0 {
			break
		}

		offset += len(songs.Song)
		bar.Add(len(songs.Song))
	}

	return tracks, nil
}

type navidromeDumpEntry struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	RawPath         string `json:"raw_path"`
	DecodedPath     string `json:"decoded_path"`
	CleanPath       string `json:"clean_path"`
	MatchPath       string `json:"match_path"`
	Title           string `json:"title,omitempty"`
	Artist          string `json:"artist,omitempty"`
	Album           string `json:"album,omitempty"`
	Rating          int    `json:"rating,omitempty"`
	PlayCount       int64  `json:"play_count,omitempty"`
	Starred         bool   `json:"starred,omitempty"`
	TrackNumber     int    `json:"track_number,omitempty"`
	DiscNumber      int    `json:"disc_number,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
	Year            int    `json:"year,omitempty"`
}

func writeNavidromeDump(path string, songs []subsonicInfo, root string, mode matchModeValue) error {
	entries := make([]navidromeDumpEntry, 0, len(songs))
	for _, song := range songs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		matchPath := normalizeMatchPathWithMode(song.Path(), root, mode)
		if *debugMode && song.Id() == "10c87dea0ab488cb39f7f607ea8c0f0d" {
			log.Printf("Navidrome dump debug for %s: raw=%q decoded=%q clean=%q normalised=%q", song.Id(), song.Path(), decoded, cleaned, matchPath)
		}
		entries = append(entries, navidromeDumpEntry{
			ID:              song.Id(),
			Path:            song.Path(),
			RawPath:         song.Path(),
			DecodedPath:     decoded,
			CleanPath:       cleaned,
			MatchPath:       matchPath,
			Title:           song.title,
			Artist:          song.artist,
			Album:           song.album,
			Rating:          song.rating,
			PlayCount:       song.playCount,
			Starred:         song.starred,
			TrackNumber:     song.trackNumber,
			DiscNumber:      song.discNumber,
			DurationSeconds: song.durationSeconds,
			Year:            song.year,
		})
	}
	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func loadNavidromeDump(path string) ([]navidromeDumpEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []navidromeDumpEntry
	if err := json.NewDecoder(file).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

type navidromeIndexEntry struct {
	ID        string
	Path      string
	CleanPath string
	MatchPath string
}

func buildNavidromeIndexFromDump(entries []navidromeDumpEntry) []navidromeIndexEntry {
	index := make([]navidromeIndexEntry, 0, len(entries))
	for _, entry := range entries {
		cleaned := firstNonEmpty(entry.CleanPath, entry.DecodedPath, entry.Path)
		if cleaned == "" {
			continue
		}
		index = append(index, navidromeIndexEntry{
			ID:        entry.ID,
			Path:      entry.Path,
			CleanPath: cleaned,
			MatchPath: entry.MatchPath,
		})
	}
	return index
}

func buildNavidromeIndexFromSongs(songs []subsonicInfo, root string, mode matchModeValue) []navidromeIndexEntry {
	index := make([]navidromeIndexEntry, 0, len(songs))
	for _, song := range songs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		matchPath := normalizeMatchPathWithMode(song.Path(), root, mode)
		index = append(index, navidromeIndexEntry{
			ID:        song.Id(),
			Path:      song.Path(),
			CleanPath: cleaned,
			MatchPath: matchPath,
		})
	}
	return index
}

func probePathInIndex(pathValue string, root string, mode matchModeValue, index []navidromeIndexEntry) {
	decoded := safePathUnescape(pathValue)
	cleaned := filepath.Clean(filepath.FromSlash(decoded))
	matchKey := normalizeMatchPathWithMode(pathValue, root, mode)
	fmt.Fprintf(stdoutWriter, "Probe path: %q\n", pathValue)
	fmt.Fprintf(stdoutWriter, "Decoded path: %q\n", decoded)
	fmt.Fprintf(stdoutWriter, "Clean path: %q\n", cleaned)
	fmt.Fprintf(stdoutWriter, "Match key: %q (root=%q mode=%q)\n", matchKey, root, mode)

	byMatch := make(map[string][]navidromeIndexEntry)
	byBasename := make(map[string][]navidromeIndexEntry)
	for _, entry := range index {
		if entry.MatchPath != "" {
			byMatch[entry.MatchPath] = append(byMatch[entry.MatchPath], entry)
		}
		base := filepath.Base(entry.CleanPath)
		base = strings.ToLower(base)
		if base != "" {
			byBasename[base] = append(byBasename[base], entry)
		}
	}

	if matches := byMatch[matchKey]; len(matches) > 0 {
		fmt.Fprintf(stdoutWriter, "Result: PASS (%d match(es))\n", len(matches))
		for i, match := range matches {
			if i >= 3 {
				break
			}
			fmt.Fprintf(stdoutWriter, "  - id=%s path=%q clean=%q\n", match.ID, match.Path, match.CleanPath)
		}
		return
	}

	fmt.Fprintln(stdoutWriter, "Result: FAIL (no exact match in Navidrome index)")
	base := strings.ToLower(filepath.Base(cleaned))
	if base != "" {
		if candidates := byBasename[base]; len(candidates) > 0 {
			fmt.Fprintln(stdoutWriter, "Nearest candidates with same basename:")
			for i, candidate := range candidates {
				if i >= 5 {
					break
				}
				fmt.Fprintf(stdoutWriter, "  - id=%s match_key=%q path=%q\n", candidate.ID, candidate.MatchPath, candidate.CleanPath)
			}
		}
	}
}

func subsonicRequest(c *subsonic.Client, endpoint string, params url.Values) error {
	resp, err := c.Request("GET", endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return &httpStatusError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	parsed := subsonic.Response{}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return err
	}
	if parsed.Error != nil {
		return &subsonicAPIError{Code: parsed.Error.Code, Message: parsed.Error.Message}
	}
	return nil
}

func chunkStrings(ids []string, size int) [][]string {
	if size <= 0 {
		return [][]string{ids}
	}

	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for len(ids) > 0 {
		if len(ids) < size {
			chunks = append(chunks, ids)
			break
		}
		chunks = append(chunks, ids[:size])
		ids = ids[size:]
	}
	return chunks
}

type locationParseResult struct {
	raw     string
	decoded string
	parsed  string
	ok      bool
	reason  string
}

func parseLocation(raw string) locationParseResult {
	result := locationParseResult{raw: raw}
	if strings.TrimSpace(raw) == "" {
		result.reason = "missing_or_invalid_location"
		return result
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" {
		pathValue := parsed.Path
		if pathValue == "" {
			pathValue = parsed.Opaque
		}
		decoded := safePathUnescape(pathValue)
		result.decoded = decoded
		if decoded == "" {
			result.reason = "missing_or_invalid_location"
			return result
		}
		parsedPath := filepath.Clean(filepath.FromSlash(decoded))
		if parsedPath == "." {
			result.reason = "missing_or_invalid_location"
			return result
		}
		result.parsed = parsedPath
		result.ok = true
		return result
	}
	if err != nil {
		result.reason = "missing_or_invalid_location"
		return result
	}
	decoded := safePathUnescape(raw)
	result.decoded = decoded
	if decoded == "" {
		result.reason = "missing_or_invalid_location"
		return result
	}
	parsedPath := filepath.Clean(filepath.FromSlash(decoded))
	if parsedPath == "." {
		result.reason = "missing_or_invalid_location"
		return result
	}
	result.parsed = parsedPath
	result.ok = true
	return result
}

func normalizeMatchPath(pathValue string, root string) string {
	return normalizeMatchPathWithMode(pathValue, root, matchModeRealpath)
}

type matchModeValue string

const (
	matchModeRealpath matchModeValue = "realpath"
	matchModeLenient  matchModeValue = "lenient"
)

func normalizeMatchPathWithMode(pathValue string, root string, mode matchModeValue) string {
	decoded := safePathUnescape(pathValue)
	normalized := filepath.Clean(filepath.FromSlash(decoded))
	normalized = normalizeUnicodePath(normalized)
	rootDecoded := safePathUnescape(root)
	rootNormalized := filepath.Clean(filepath.FromSlash(rootDecoded))
	rootNormalized = normalizeUnicodePath(rootNormalized)
	if rootNormalized == "." || rootNormalized == string(os.PathSeparator) {
		rootNormalized = ""
	}
	normalized = strings.TrimLeft(normalized, string(os.PathSeparator))
	rootNormalized = strings.TrimLeft(rootNormalized, string(os.PathSeparator))

	normalizedLower := strings.ToLower(normalized)
	rootLower := strings.ToLower(rootNormalized)
	if rootLower != "" {
		prefix := rootLower
		if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
			prefix += string(os.PathSeparator)
		}
		if strings.HasPrefix(normalizedLower, prefix) {
			normalizedLower = strings.TrimPrefix(normalizedLower, prefix)
		} else if strings.HasPrefix(normalizedLower, rootLower) {
			normalizedLower = strings.TrimPrefix(normalizedLower, rootLower)
		}
	}

	if mode == matchModeLenient {
		normalizedLower = normalizeTrackDash(normalizedLower)
	}

	return strings.TrimLeft(normalizedLower, string(os.PathSeparator))
}

func normalizeUnicodePath(value string) string {
	if value == "" {
		return value
	}
	return norm.NFC.String(value)
}

func normalizeTrackDash(pathValue string) string {
	if pathValue == "" {
		return pathValue
	}
	dir, base := filepath.Split(pathValue)
	if base == "" {
		return pathValue
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = trackDashRegex.ReplaceAllString(name, "$1 ")
	base = name + ext
	return dir + base
}

var trackDashRegex = regexp.MustCompile(`^(\d+)\s*[-–—]\s*`)

func safePathUnescape(value string) string {
	if value == "" {
		return value
	}
	decoded, err := url.PathUnescape(value)
	if err == nil {
		return decoded
	}
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch == '%' && i+2 < len(value) {
			if hex, ok := decodeHexByte(value[i+1], value[i+2]); ok {
				b.WriteByte(hex)
				i += 2
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var revision string
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
			break
		}
	}
	if revision != "" {
		return revision
	}
	return info.Main.Version
}

func buildGitCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func decodeHexByte(a, b byte) (byte, bool) {
	hi, ok := hexValue(a)
	if !ok {
		return 0, false
	}
	lo, ok := hexValue(b)
	if !ok {
		return 0, false
	}
	return (hi << 4) | lo, true
}

func hexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func matchesFilter(value string, filter string) bool {
	if filter == "" {
		return true
	}
	value = strings.ToLower(value)
	for _, part := range strings.Split(filter, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if strings.Contains(value, strings.ToLower(trimmed)) {
			return true
		}
	}
	return false
}

func isRemoteTrack(track itunes.Track) bool {
	if strings.EqualFold(track.TrackType, "Remote") {
		return true
	}
	if track.AppleMusic {
		return true
	}
	return strings.TrimSpace(track.Location) == ""
}

func isInvalidParsedPath(pathValue string) bool {
	if strings.TrimSpace(pathValue) == "" {
		return true
	}
	return pathValue == "." || pathValue == string(os.PathSeparator)
}

func normalizeRootPath(root string) string {
	if root == "" {
		return ""
	}
	decoded := safePathUnescape(root)
	cleaned := filepath.Clean(filepath.FromSlash(decoded))
	cleaned = normalizeUnicodePath(cleaned)
	if cleaned == "." {
		return ""
	}
	if cleaned != string(os.PathSeparator) {
		cleaned = strings.TrimRight(cleaned, string(os.PathSeparator))
	}
	return cleaned
}

func normalizeMusicRootPath(root string) string {
	normalized, _ := normalizeMusicRootPathWithInfo(root)
	return normalized
}

func normalizeMusicRootPathWithInfo(root string) (string, bool) {
	normalized := normalizeRootPath(root)
	if normalized == "" {
		return "", false
	}
	if looksLikeAudioFile(normalized) {
		return normalizeRootPath(filepath.Dir(normalized)), true
	}
	info, err := os.Stat(normalized)
	if err == nil && !info.IsDir() {
		return normalizeRootPath(filepath.Dir(normalized)), true
	}
	return normalized, false
}

var defaultExtensions = []string{".mp3", ".m4a", ".flac", ".ogg", ".opus", ".aac", ".wav", ".aiff", ".alac"}

func looksLikeAudioFile(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".mp3", ".m4a", ".flac", ".ogg", ".opus", ".aac", ".wav", ".aiff", ".alac":
		return true
	default:
		return false
	}
}

func parseExtensions(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return append([]string{}, defaultExtensions...)
	}
	parts := strings.Split(raw, ",")
	extensions := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.ToLower(strings.TrimSpace(part))
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		extensions = append(extensions, trimmed)
	}
	return extensions
}

func extensionAllowlist(extensions []string) map[string]struct{} {
	allowlist := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		if ext == "" {
			continue
		}
		allowlist[strings.ToLower(ext)] = struct{}{}
	}
	return allowlist
}

func extensionOfPath(pathValue string) string {
	return strings.ToLower(filepath.Ext(pathValue))
}

func isExtensionAllowed(pathValue string, allowlist map[string]struct{}) (string, bool) {
	ext := extensionOfPath(pathValue)
	if ext == "" {
		return ext, false
	}
	_, ok := allowlist[ext]
	return ext, ok
}

func coerceNonRootPath(root string) (string, bool) {
	normalized := normalizeRootPath(root)
	if normalized == string(os.PathSeparator) {
		return "", true
	}
	return normalized, false
}

func buildRelativePathSet(songs []itunesInfo, root string) map[string]struct{} {
	paths := make(map[string]struct{}, len(songs))
	for _, song := range songs {
		rel := normalizeMatchPathWithMode(song.Path(), root, matchModeRealpath)
		if rel == "" {
			continue
		}
		paths[rel] = struct{}{}
	}
	return paths
}

type pathCheckResult struct {
	decoded    string
	cleaned    string
	relative   string
	isAbsolute bool
	isReal     bool
	reason     string
}

func validateNavidromePath(raw string, musicRoot string, srcRelativePaths map[string]struct{}) pathCheckResult {
	decoded := safePathUnescape(raw)
	cleaned := filepath.Clean(filepath.FromSlash(decoded))
	cleaned = normalizeUnicodePath(cleaned)
	if cleaned == "." {
		cleaned = ""
	}

	result := pathCheckResult{
		decoded:    decoded,
		cleaned:    cleaned,
		isAbsolute: filepath.IsAbs(cleaned),
		isReal:     true,
	}

	musicRoot = normalizeRootPath(musicRoot)
	if result.isAbsolute {
		if musicRoot != "" {
			rootLower := strings.ToLower(musicRoot)
			cleanLower := strings.ToLower(cleaned)
			if cleanLower != rootLower {
				prefix := rootLower
				if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
					prefix += string(os.PathSeparator)
				}
				if !strings.HasPrefix(cleanLower, prefix) {
					result.isReal = false
					result.reason = "absolute path is outside the configured music root"
					return result
				}
			}
		}
		return result
	}

	relative := strings.TrimLeft(cleaned, string(os.PathSeparator))
	result.relative = relative
	base := filepath.Base(relative)
	relativeKey := strings.ToLower(relative)
	if trackDashRegex.MatchString(base) {
		if len(srcRelativePaths) == 0 {
			result.isReal = false
			result.reason = "relative path uses a track-number dash pattern"
			return result
		}
		if _, ok := srcRelativePaths[relativeKey]; !ok {
			result.isReal = false
			result.reason = "relative path uses a track-number dash pattern"
			return result
		}
	}

	if len(srcRelativePaths) > 0 {
		if _, ok := srcRelativePaths[relativeKey]; ok {
			return result
		}
		result.isReal = false
		result.reason = "relative path not found in source library"
		return result
	}

	result.isReal = false
	result.reason = "relative path without configured music root"
	return result
}

func commonDirPrefix(a, b string) string {
	aClean := filepath.Clean(filepath.FromSlash(safePathUnescape(a)))
	bClean := filepath.Clean(filepath.FromSlash(safePathUnescape(b)))

	if aClean == "." || bClean == "." {
		return ""
	}

	aAbs := filepath.IsAbs(aClean)
	bAbs := filepath.IsAbs(bClean)
	aParts := strings.Split(strings.Trim(aClean, string(os.PathSeparator)), string(os.PathSeparator))
	bParts := strings.Split(strings.Trim(bClean, string(os.PathSeparator)), string(os.PathSeparator))

	limit := len(aParts)
	if len(bParts) < limit {
		limit = len(bParts)
	}

	commonParts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		if strings.EqualFold(aParts[i], bParts[i]) {
			commonParts = append(commonParts, aParts[i])
		} else {
			break
		}
	}

	if len(commonParts) == 0 {
		return ""
	}

	prefix := strings.Join(commonParts, string(os.PathSeparator))
	if aAbs || bAbs {
		prefix = string(os.PathSeparator) + prefix
	}
	return filepath.Clean(prefix)
}

func deriveMusicRoot(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := paths[0]
	for _, p := range paths[1:] {
		prefix = commonDirPrefix(prefix, p)
		if prefix == "" {
			break
		}
	}
	return normalizeRootPath(prefix)
}

func buildStarUpdates(pairs map[string]*songPair) ([]string, []string) {
	var toStar []string
	var toUnstar []string
	for _, v := range pairs {
		if v.src.Id() == "" || v.dst.Id() == "" {
			continue
		}
		srcLoved := v.src.IsFavourite()
		if srcLoved && !v.dst.starred {
			toStar = append(toStar, v.dst.Id())
		}
		if !srcLoved && v.dst.starred {
			toUnstar = append(toUnstar, v.dst.Id())
		}
	}
	return toStar, toUnstar
}

func buildUnstarCandidates(pairs map[string]*songPair) []unstarCandidate {
	candidates := make([]unstarCandidate, 0)
	for _, v := range pairs {
		if v.src.Id() == "" || v.dst.Id() == "" {
			continue
		}
		if v.dst.starred && !v.src.IsFavourite() {
			candidates = append(candidates, unstarCandidate{
				navidrome: v.dst,
				apple:     v.src,
				reason:    reasonStarredNotLoved,
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a := candidates[i]
		b := candidates[j]
		if a.navidrome.artist != b.navidrome.artist {
			return a.navidrome.artist < b.navidrome.artist
		}
		if a.navidrome.album != b.navidrome.album {
			return a.navidrome.album < b.navidrome.album
		}
		if a.navidrome.title != b.navidrome.title {
			return a.navidrome.title < b.navidrome.title
		}
		if a.navidrome.path != b.navidrome.path {
			return a.navidrome.path < b.navidrome.path
		}
		return a.navidrome.id < b.navidrome.id
	})
	return candidates
}

func runDirFromLogFile() string {
	if *logFile != "" {
		return filepath.Dir(*logFile)
	}
	return "."
}

func writeUnstarAuditTSV(path string, entries []unstarCandidate, mode matchModeValue) error {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		appleID := ""
		if entry.apple.id != 0 {
			appleID = strconv.Itoa(entry.apple.id)
		}
		rows = append(rows, []string{
			"unstar",
			entry.navidrome.id,
			appleID,
			entry.navidrome.artist,
			entry.navidrome.album,
			entry.navidrome.title,
			entry.navidrome.path,
			entry.reason,
			string(mode),
			"matched",
		})
	}
	return report.WriteTSV(path, []string{
		"op",
		"navidrome_id",
		"apple_track_id",
		"artist",
		"album",
		"title",
		"path",
		"reason_code",
		"match_mode",
		"match_confidence",
	}, rows)
}

func unstarIDs(candidates []unstarCandidate) []string {
	ids := make([]string, 0, len(candidates))
	seen := make(map[string]struct{})
	for _, entry := range candidates {
		if entry.navidrome.id == "" {
			continue
		}
		if _, ok := seen[entry.navidrome.id]; ok {
			continue
		}
		seen[entry.navidrome.id] = struct{}{}
		ids = append(ids, entry.navidrome.id)
	}
	return ids
}

func writeMissingReport(path string, report missingReport) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "missing-*.json")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func buildMissingSongFromSrc(src itunesInfo, includeDebug bool) *missingSong {
	if src.Id() == "" {
		return nil
	}
	cleaned := filepath.Clean(filepath.FromSlash(src.Path()))
	ext := extensionOfPath(cleaned)
	if ext == "" {
		ext = "<none>"
	}
	entry := &missingSong{
		ID:        src.Id(),
		Path:      src.Path(),
		Name:      src.name,
		Artist:    src.artist,
		Album:     src.album,
		CleanPath: cleaned,
		Extension: ext,
	}
	if includeDebug {
		decoded := safePathUnescape(src.Path())
		entry.RawPath = src.Path()
		entry.DecodedPath = decoded
		entry.CleanPath = filepath.Clean(filepath.FromSlash(decoded))
	}
	return entry
}

func buildMissingSongFromTrack(track itunes.Track, location locationParseResult, includeDebug bool) *missingSong {
	if track.TrackId == 0 {
		return nil
	}
	entry := &missingSong{
		ID:     strconv.Itoa(track.TrackId),
		Path:   location.parsed,
		Name:   track.Name,
		Artist: track.Artist,
		Album:  track.Album,
	}
	if location.parsed != "" {
		cleaned := filepath.Clean(filepath.FromSlash(location.parsed))
		entry.CleanPath = cleaned
		entry.Extension = extensionOfPath(cleaned)
		if entry.Extension == "" {
			entry.Extension = "<none>"
		}
	}
	if includeDebug {
		entry.RawPath = location.raw
		entry.DecodedPath = location.decoded
		entry.CleanPath = filepath.Clean(filepath.FromSlash(location.parsed))
	}
	return entry
}

const srcSampleLimit = 20
const notInNavidromeSampleLimit = 12
const notInNavidromeDirSegments = 2

func appendSample(samples *[]missingSong, entry *missingSong) {
	if entry == nil {
		return
	}
	if len(*samples) >= srcSampleLimit {
		return
	}
	*samples = append(*samples, *entry)
}

func appendMissingEntrySample(samples *[]missingEntry, entry missingEntry) {
	if len(*samples) >= notInNavidromeSampleLimit {
		return
	}
	*samples = append(*samples, entry)
}

func directoryPrefixAfterRoot(pathValue string, root string, segments int) string {
	if pathValue == "" {
		return ""
	}
	cleaned := normalizeUnicodePath(filepath.Clean(filepath.FromSlash(pathValue)))
	if cleaned == "." {
		return ""
	}
	rootClean := normalizeUnicodePath(filepath.Clean(filepath.FromSlash(root)))
	relative := strings.TrimLeft(cleaned, string(os.PathSeparator))
	if rootClean != "" {
		cleanedLower := strings.ToLower(cleaned)
		rootLower := strings.ToLower(rootClean)
		if strings.HasPrefix(cleanedLower, rootLower) {
			relative = strings.TrimLeft(cleaned[len(rootClean):], string(os.PathSeparator))
		}
	}
	relative = strings.TrimLeft(relative, string(os.PathSeparator))
	if relative == "" {
		return ""
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	if segments <= 0 || segments > len(parts) {
		segments = len(parts)
	}
	return filepath.Join(parts[:segments]...)
}

func buildMissingSongFromDst(dst subsonicInfo, includeDebug bool) *missingSong {
	if dst.Id() == "" {
		return nil
	}
	entry := &missingSong{
		ID:   dst.Id(),
		Path: dst.Path(),
	}
	if includeDebug {
		decoded := safePathUnescape(dst.Path())
		entry.RawPath = dst.Path()
		entry.DecodedPath = decoded
		entry.CleanPath = filepath.Clean(filepath.FromSlash(decoded))
	}
	return entry
}

func runAnalyseDump(dumpPath string, missingPath string, reportPath string) error {
	summary, err := analyseDumpFile(dumpPath)
	if err != nil {
		return err
	}

	if missingPath != "" {
		analysis, err := analyseMissingReport(missingPath)
		if err != nil {
			return err
		}
		summary.MissingReasonCounts = analysis.ReasonCounts
		summary.MissingExamples = analysis.ReasonExamples
		summary.NotInNavidromeIndexByExt = analysis.NotInNavidromeIndexByExt
		summary.NotInNavidromeIndexByDir = analysis.NotInNavidromeIndexByDir
		summary.NotInNavidromeIndexExamples = analysis.NotInNavidromeExamples
	}

	report := dumpAnalysisReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		DumpPath:    dumpPath,
		Summary:     summary,
	}

	printAnalyseSummary(summary)

	if reportPath != "" {
		return writeAnalyseReport(reportPath, report)
	}
	return nil
}

func analyseDumpFile(path string) (analyseSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return analyseSummary{}, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	tok, err := decoder.Token()
	if err != nil {
		return analyseSummary{}, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return analyseSummary{}, fmt.Errorf("expected JSON array in dump file %s", path)
	}

	extCounts := make(map[string]int)
	topDirs := make(map[string]int)
	var total, absCount, relCount int
	for decoder.More() {
		var entry struct {
			Path        string `json:"path"`
			DecodedPath string `json:"decoded_path"`
			CleanPath   string `json:"clean_path"`
		}
		if err := decoder.Decode(&entry); err != nil {
			return analyseSummary{}, err
		}
		total++
		pathValue := firstNonEmpty(entry.CleanPath, entry.DecodedPath, entry.Path)
		if pathValue == "" {
			continue
		}
		decoded := safePathUnescape(pathValue)
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		cleaned = normalizeUnicodePath(cleaned)
		if cleaned == "." {
			continue
		}
		if filepath.IsAbs(cleaned) {
			absCount++
		} else {
			relCount++
		}
		ext := extensionOfPath(cleaned)
		if ext == "" {
			ext = "<none>"
		}
		extCounts[ext]++
		if top := topDirectory(cleaned); top != "" {
			topDirs[top]++
		}
	}

	return analyseSummary{
		Total:           total,
		AbsoluteCount:   absCount,
		RelativeCount:   relCount,
		ExtensionCounts: extCounts,
		TopDirectories:  topDirs,
	}, nil
}

func analyseMissingReport(path string) (missingAnalysis, error) {
	file, err := os.Open(path)
	if err != nil {
		return missingAnalysis{}, err
	}
	defer file.Close()

	var report missingReport
	if err := json.NewDecoder(file).Decode(&report); err != nil {
		return missingAnalysis{}, err
	}

	reasonCounts := make(map[string]int)
	reasonExamples := make(map[string][]missingEntrySummary)
	for _, entry := range report.Missing {
		reasonCounts[entry.Reason]++
		if len(reasonExamples[entry.Reason]) >= 3 {
			continue
		}
		var pathValue string
		if entry.Src != nil {
			pathValue = entry.Src.Path
		} else if entry.Dst != nil {
			pathValue = entry.Dst.Path
		}
		reasonExamples[entry.Reason] = append(reasonExamples[entry.Reason], missingEntrySummary{
			Side:     entry.Side,
			Reason:   entry.Reason,
			MatchKey: entry.MatchKey,
			Path:     pathValue,
		})
	}

	notInNavidromeExamples := make([]missingEntrySummary, 0, len(report.NotInNavidromeIndexSamples))
	for _, entry := range report.NotInNavidromeIndexSamples {
		pathValue := ""
		if entry.Src != nil {
			pathValue = entry.Src.Path
		}
		notInNavidromeExamples = append(notInNavidromeExamples, missingEntrySummary{
			Side:     entry.Side,
			Reason:   entry.Reason,
			MatchKey: entry.MatchKey,
			Path:     pathValue,
		})
	}
	if len(notInNavidromeExamples) == 0 && len(report.Missing) > 0 {
		for _, entry := range report.Missing {
			if entry.Reason != "not_in_navidrome_index" {
				continue
			}
			pathValue := ""
			if entry.Src != nil {
				pathValue = entry.Src.Path
			}
			notInNavidromeExamples = append(notInNavidromeExamples, missingEntrySummary{
				Side:     entry.Side,
				Reason:   entry.Reason,
				MatchKey: entry.MatchKey,
				Path:     pathValue,
			})
			if len(notInNavidromeExamples) >= 3 {
				break
			}
		}
	}

	return missingAnalysis{
		ReasonCounts:             reasonCounts,
		ReasonExamples:           reasonExamples,
		NotInNavidromeIndexByExt: report.NotInNavidromeIndexByExt,
		NotInNavidromeIndexByDir: report.NotInNavidromeIndexByDir,
		NotInNavidromeExamples:   notInNavidromeExamples,
	}, nil
}

func topDirectory(pathValue string) string {
	cleaned := strings.TrimLeft(pathValue, string(os.PathSeparator))
	if cleaned == "" {
		return ""
	}
	parts := strings.Split(cleaned, string(os.PathSeparator))
	if len(parts) == 1 {
		return parts[0]
	}
	return filepath.Join(parts[0], parts[1])
}

func printAnalyseSummary(summary analyseSummary) {
	fmt.Fprintln(stdoutWriter, "== Dump Analysis ==")
	fmt.Fprintf(stdoutWriter, "Total entries: %d\n", summary.Total)
	fmt.Fprintf(stdoutWriter, "Absolute paths: %d\n", summary.AbsoluteCount)
	fmt.Fprintf(stdoutWriter, "Relative paths: %d\n", summary.RelativeCount)

	fmt.Fprintln(stdoutWriter, "Top extensions:")
	for _, item := range sortCounts(summary.ExtensionCounts, 12) {
		fmt.Fprintf(stdoutWriter, "  %s: %d\n", item.Key, item.Value)
	}

	fmt.Fprintln(stdoutWriter, "Top directories:")
	for _, item := range sortCounts(summary.TopDirectories, 10) {
		fmt.Fprintf(stdoutWriter, "  %s: %d\n", item.Key, item.Value)
	}

	if len(summary.MissingReasonCounts) > 0 {
		fmt.Fprintln(stdoutWriter, "Missing reasons:")
		for _, item := range sortCounts(summary.MissingReasonCounts, 12) {
			fmt.Fprintf(stdoutWriter, "  %s: %d\n", item.Key, item.Value)
			if examples := summary.MissingExamples[item.Key]; len(examples) > 0 {
				for _, example := range examples {
					fmt.Fprintf(stdoutWriter, "    - %s (%s)\n", example.MatchKey, example.Path)
				}
			}
		}
	}

	if len(summary.NotInNavidromeIndexByExt) > 0 {
		fmt.Fprintln(stdoutWriter, "Not in Navidrome index (by extension):")
		for _, item := range sortCounts(summary.NotInNavidromeIndexByExt, 12) {
			fmt.Fprintf(stdoutWriter, "  %s: %d\n", item.Key, item.Value)
		}
	}

	if len(summary.NotInNavidromeIndexByDir) > 0 {
		fmt.Fprintln(stdoutWriter, "Not in Navidrome index (top directories):")
		for _, item := range sortCounts(summary.NotInNavidromeIndexByDir, 10) {
			fmt.Fprintf(stdoutWriter, "  %s: %d\n", item.Key, item.Value)
		}
	}

	if len(summary.NotInNavidromeIndexExamples) > 0 {
		fmt.Fprintln(stdoutWriter, "Not in Navidrome index (examples):")
		for _, example := range summary.NotInNavidromeIndexExamples {
			fmt.Fprintf(stdoutWriter, "  - %s (%s)\n", example.MatchKey, example.Path)
		}
	}
}

type sortedCount struct {
	Key   string
	Value int
}

func sortCounts(counts map[string]int, limit int) []sortedCount {
	items := make([]sortedCount, 0, len(counts))
	for key, value := range counts {
		items = append(items, sortedCount{Key: key, Value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Value == items[j].Value {
			return items[i].Key < items[j].Key
		}
		return items[i].Value > items[j].Value
	})
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitCountsMap(counts map[string]int, limit int) map[string]int {
	if limit <= 0 || len(counts) <= limit {
		return counts
	}
	limited := make(map[string]int, limit)
	for _, item := range sortCounts(counts, limit) {
		limited[item.Key] = item.Value
	}
	return limited
}

func writeAnalyseReport(path string, report dumpAnalysisReport) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

//func writeNavidromeSql(f io.Writer, tracks map[string]*track) error {
//	fmt.Fprintln(f, "# sqlite3 navidrome.db < this_file.sql")
//	fmt.Fprintln(f, "# Or if using Docker...")
//	fmt.Fprintln(f, "# docker run --rm -i --user 0 -v navidrome_data:/data keinos/sqlite3:latest sqlite3 /data/navidrome.db < this_file.sql")
//
//	// Wrap everything in a transacation so it's not slow.
//	fmt.Fprintln(f, "BEGIN TRANSACTION;")
//	for _, v := range tracks {
//		if v.itunesCreated.IsZero() || v.subsonicId == "" {
//			continue
//		}
//
//		fmt.Fprintf(f, "UPDATE media_file SET created_at = datetime(%d, 'unixepoch') WHERE id='%s';\n", v.itunesCreated.Unix(), v.subsonicId)
//	}
//	fmt.Fprintln(f, "COMMIT;")
//
//	return nil
//}

func main() {
	flag.Parse()
	if *applyFlag && *verifyFlag {
		log.Fatal("--apply and --verify cannot be used together")
	}
	if *analyseDump != "" {
		if err := runAnalyseDump(*analyseDump, *analyseMissing, *analyseReport); err != nil {
			log.Fatalf("Failed to analyse dump %q: %s", *analyseDump, err)
		}
		return
	}
	setFlags := collectSetFlags()
	applyDryRunOverrides(setFlags, *applyFlag)
	cfg := loadConfig()
	var presetConfig preset
	if *presetName != "" || *dumpPreset {
		if *configFile == "" {
			log.Fatalf("--preset/--dump_preset requires --config to be set")
		}
		presetFileCfg, err := loadPresetFile(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config %q: %s", *configFile, err)
		}
		if *presetName == "" {
			log.Fatalf("--dump_preset requires --preset to be set")
		}
		value, err := resolvePreset(*presetName, presetFileCfg)
		if err != nil {
			log.Fatalf("Failed to resolve preset %q: %s", *presetName, err)
		}
		presetConfig = value
		applyPreset(presetConfig, setFlags)
		if *dumpPreset {
			resolved := buildResolvedPreset(*presetName, presetConfig, setFlags, cfg)
			if err := writeResolvedPreset(resolved); err != nil {
				log.Fatalf("Failed to write resolved preset: %s", err)
			}
			return
		}
	}
	if *presetName != "" {
		if err := checkPresetPlaceholders(presetConfig, setFlags, cfg); err != nil {
			resolved := buildResolvedPreset(*presetName, presetConfig, setFlags, cfg)
			_ = writeResolvedPreset(resolved)
			log.Fatalf("Preset %q looks like it contains placeholder values: %s\nUpdate configs or override with CLI flags (see resolved preset above).", *presetName, err)
		}
	}
	if *verifyFlag {
		*auditFlag = true
		*verifySrcFiles = true
		*dryRun = true
	}
	if !*dryRun {
		clearReportFlagsForApply(setFlags)
	}
	filterActive := *filterAlbum != "" || *filterArtist != "" || *filterName != "" || *filterPath != "" || *limitTracks > 0
	var logFileHandle *os.File
	if *logFile != "" {
		logDir := filepath.Dir(*logFile)
		if logDir != "." {
			if err := os.MkdirAll(logDir, 0o700); err != nil {
				log.Fatalf("Failed to create log directory %q: %s", logDir, err)
			}
		}
		handle, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			log.Fatalf("Failed to open log file %q: %s", *logFile, err)
		}
		logFileHandle = handle
		multi := io.MultiWriter(os.Stderr, logFileHandle)
		log.SetOutput(multi)
		stdoutWriter = io.MultiWriter(os.Stdout, logFileHandle)
		stderrWriter = io.MultiWriter(os.Stderr, logFileHandle)
	}
	if logFileHandle != nil {
		defer logFileHandle.Close()
	}
	if *explainNotApplied {
		runDirValue := *runDir
		if runDirValue == "" {
			runDirValue = filepath.Join("run", "latest")
		}
		if err := explainNotAppliedFromRunDir(runDirValue, *explainNotAppliedTopN); err != nil {
			log.Fatalf("Failed to explain not-applied rows: %s", err)
		}
		return
	}
	if *reportLibrary != "" {
		filters := filterOptions{
			album:  *filterAlbum,
			artist: *filterArtist,
			name:   *filterName,
			path:   *filterPath,
			limit:  *limitTracks,
		}
		if err := runReportLibraryStats(*itunesXml, filters, *reportLibrary, *reportOutTSV); err != nil {
			log.Fatalf("Failed to report library stats: %s", err)
		}
		if *dryRun {
			return
		}
	}
	if *reportRemoteStreaming != "" && *dumpFile != "" && !*auditFlag && *reportSyncPlan == "" && *reportReconcile == "" && *reportRemoteMatchJSON == "" && *reportRemoteMatchTSV == "" && *reportRemoteActionable == "" {
		if !*dryRun {
			log.Fatal("--report_remote_streaming_gaps is read-only; use --dry_run=true")
		}
		if err := runReportRemoteStreamingGaps(nil, *itunesXml, *dumpFile, *reportRemoteStreaming); err != nil {
			log.Fatalf("Failed to write remote streaming gaps report: %s", err)
		}
		return
	}
	subsonicUser := firstNonEmpty(os.Getenv("SUBSONIC_USER"), presetConfig.SubsonicUser, cfg.SubsonicUser)
	subsonicPass := firstNonEmpty(os.Getenv("SUBSONIC_PASS"), presetConfig.SubsonicPass, cfg.SubsonicPass)
	if *subsonicUrl == "" {
		*subsonicUrl = cfg.SubsonicURL
	}

	requiresNonInteractive := *auditFlag || *reportSyncPlan != "" || *reportReconcile != "" || *reportRemoteMatchJSON != "" || *reportRemoteMatchTSV != "" || *reportRemoteActionable != "" || (*reportRemoteStreaming != "" && *dumpFile == "")
	if requiresNonInteractive {
		missing := make([]string, 0)
		if *subsonicUrl == "" {
			missing = append(missing, "--subsonic")
		}
		if subsonicUser == "" {
			missing = append(missing, "SUBSONIC_USER")
		}
		if subsonicPass == "" {
			missing = append(missing, "SUBSONIC_PASS")
		}
		if len(missing) > 0 {
			log.Fatalf("Missing required credentials for audit/reporting mode: %s. Provide via CLI flags, environment variables, or presets/config.", strings.Join(missing, ", "))
		}
	}

	reader := bufio.NewReader(os.Stdin)
	if *subsonicUrl == "" {
		value, err := promptInput(reader, "Navidrome URL")
		if err != nil {
			log.Fatalf("Failed to read Navidrome URL: %s", err)
		}
		*subsonicUrl = value
	}
	if subsonicUser == "" {
		value, err := promptInput(reader, "Navidrome Username")
		if err != nil {
			log.Fatalf("Failed to read Navidrome username: %s", err)
		}
		subsonicUser = value
	}
	if subsonicPass == "" {
		value, err := promptPassword("Navidrome Password")
		if err != nil {
			log.Fatalf("Failed to read Navidrome password: %s", err)
		}
		subsonicPass = value
	}

	if *subsonicUrl == "" || subsonicUser == "" || subsonicPass == "" {
		log.Fatal("Navidrome URL, username, and password are required.")
	}

	selectedMatchMode := matchModeValue(*matchMode)
	switch selectedMatchMode {
	case matchModeRealpath, matchModeLenient:
	default:
		log.Fatalf("Invalid --match_mode %q (expected %q or %q).", *matchMode, matchModeRealpath, matchModeLenient)
	}

	extensions := parseExtensions(*extensionsFlag)
	allowlist := extensionAllowlist(extensions)
	if len(allowlist) == 0 {
		log.Fatal("No valid extensions were provided.")
	}

	cfg.SubsonicURL = *subsonicUrl
	cfg.SubsonicUser = subsonicUser
	cfg.SubsonicPass = subsonicPass
	if err := saveConfig(cfg); err != nil {
		log.Printf("Warning: failed to save config: %s", err)
	}

	var srcSongs []itunesInfo
	var playlistRefs []playlistRef
	var matchedCount int
	var srcTotal int
	srcRemoteCount := 0
	srcInvalidLocationCount := 0
	srcExcludedExtensionCount := 0
	srcStaleMissingCount := 0
	srcPathMismatchCount := 0
	var invalidSrcSamples []missingSong
	var remoteSrcSamples []missingSong
	var excludedExtensionSamples []missingSong
	var staleSrcSamples []missingSong
	var pathMismatchSamples []missingSong
	missingEntries := make([]missingEntry, 0)
	notInNavidromeSamples := make([]missingEntry, 0)
	notInNavidromeByExt := make(map[string]int)
	notInNavidromeByDir := make(map[string]int)
	var derivedMusicRoot string
	var derivedMusicRootSource string
	needsLocalScan := !*auditFlag && *reportSyncPlan == "" && *reportReconcile == "" && *reportRemoteMatchJSON == "" && *reportRemoteMatchTSV == "" && *reportRemoteActionable == "" && *reportRemoteStreaming == ""
	if needsLocalScan && *itunesXml != "" {
		f, err := os.Open(*itunesXml)
		defer f.Close()
		if err != nil {
			log.Fatalf("failed to open --itunes_xml=%s: %s", *itunesXml, err)
		}
		library, err := itunes.LoadLibrary(f)
		if err != nil {
			log.Fatalf("failed to read library: %s", err)
		}
		for _, playlist := range library.Playlists {
			playlistRefs = append(playlistRefs, playlistRef{Name: playlist.Name, Master: playlist.Master, Items: playlist.PlaylistItems})
		}

		for _, v := range library.Tracks {
			location := parseLocation(v.Location)
			filterPathValue := location.parsed
			if filterPathValue == "" {
				if location.decoded != "" {
					filterPathValue = location.decoded
				} else {
					filterPathValue = location.raw
				}
			}

			if !matchesFilter(v.Album, *filterAlbum) || !matchesFilter(v.Artist, *filterArtist) || !matchesFilter(v.Name, *filterName) || !matchesFilter(filterPathValue, *filterPath) {
				continue
			}
			if *limitTracks > 0 && matchedCount >= *limitTracks {
				break
			}
			matchedCount++
			srcTotal++

			if isRemoteTrack(v) {
				srcRemoteCount++
				appendSample(&remoteSrcSamples, buildMissingSongFromTrack(v, location, true))
				continue
			}

			if !location.ok || isInvalidParsedPath(location.parsed) {
				srcInvalidLocationCount++
				appendSample(&invalidSrcSamples, buildMissingSongFromTrack(v, location, true))
				continue
			}

			ext, allowed := isExtensionAllowed(location.parsed, allowlist)
			if !allowed {
				if ext == "" {
					ext = "<none>"
				}
				srcExcludedExtensionCount++
				appendSample(&excludedExtensionSamples, buildMissingSongFromTrack(v, location, true))
				continue
			}

			if *verifySrcFiles {
				if _, err := os.Stat(location.parsed); err != nil {
					if *pathResolveOnDisk {
						if resolved, ok := resolvePathOnDisk(location.parsed); ok {
							srcPathMismatchCount++
							sample := buildMissingSongFromTrack(v, location, true)
							if sample != nil {
								sample.ResolvedPath = resolved
								appendSample(&pathMismatchSamples, sample)
							}
							continue
						}
					}
					srcStaleMissingCount++
					appendSample(&staleSrcSamples, buildMissingSongFromTrack(v, location, true))
					continue
				}
			}

			srcSongs = append(srcSongs, itunesInfo{
				id:        v.TrackId,
				path:      location.parsed,
				name:      v.Name,
				artist:    v.Artist,
				album:     v.Album,
				rating:    v.Rating,
				playDate:  v.PlayDateUTC,
				dateAdded: v.DateAdded,
				playCount: v.PlayCount,
				loved:     v.Loved != nil && *v.Loved,
				favorited: v.Favorited != nil && *v.Favorited,
				hasLoved:  v.Loved != nil,
				hasFav:    v.Favorited != nil,
			})
		}

		if *musicRoot != "" {
			var warned bool
			derivedMusicRoot, warned = normalizeMusicRootPathWithInfo(*musicRoot)
			derivedMusicRootSource = "--music_root"
			if warned {
				log.Printf("Warning: music_root %q looks like a file; using %q instead.", *musicRoot, derivedMusicRoot)
			}
		} else if *itunesRoot != "" {
			var warned bool
			derivedMusicRoot, warned = normalizeMusicRootPathWithInfo(*itunesRoot)
			derivedMusicRootSource = "--itunes_root"
			if warned {
				log.Printf("Warning: itunes_root %q looks like a file; using %q for music_root derivation.", *itunesRoot, derivedMusicRoot)
			}
		} else {
			paths := make([]string, 0, len(srcSongs))
			for _, song := range srcSongs {
				if song.Path() == "" {
					continue
				}
				paths = append(paths, song.Path())
			}
			var warned bool
			derivedMusicRoot, warned = normalizeMusicRootPathWithInfo(deriveMusicRoot(paths))
			if derivedMusicRoot != "" {
				derivedMusicRootSource = "iTunes paths"
			}
			if warned {
				log.Printf("Warning: derived music root looks like a file; using %q instead.", derivedMusicRoot)
			}
		}
		if coerced, warned := coerceNonRootPath(derivedMusicRoot); warned {
			log.Printf("Warning: detected music root was '/', treating as unknown to avoid incorrect trimming.")
			derivedMusicRoot = coerced
		}
		if filterActive && derivedMusicRoot != "" && derivedMusicRootSource != "" {
			log.Printf("Derived music_root=%q from %s (filters active).", derivedMusicRoot, derivedMusicRootSource)
		} else if filterActive && derivedMusicRoot == "" && *debugMode {
			log.Printf("Unable to derive music_root with filters active; diagnostics will omit root-based prefixes.")
		}
	}

	c := &subsonic.Client{
		Client:     &http.Client{},
		BaseUrl:    *subsonicUrl,
		User:       subsonicUser,
		ClientName: *subsonicClient,
	}
	if err := c.Authenticate(subsonicPass); err != nil {
		log.Fatalf("Failed to create Navidrome client: %s", err)
	}

	if *debugMode {
		log.Printf("Filters: album=%q artist=%q name=%q path=%q limit=%d", *filterAlbum, *filterArtist, *filterName, *filterPath, *limitTracks)
		log.Printf("Matching: mode=%q require_real_path=%t music_root=%q", selectedMatchMode, *requireRealPath, derivedMusicRoot)
		log.Printf("Extensions: %s", strings.Join(extensions, ","))
		log.Printf("Verify src files: %t", *verifySrcFiles)
		log.Printf("Subsonic client: c=%q", *subsonicClient)
	}

	if *auditFlag {
		paths := auditRemoteMatchPaths{
			jsonPath:       *reportRemoteMatchJSON,
			tsvPath:        *reportRemoteMatchTSV,
			actionableTSV:  *reportRemoteActionable,
			includeLowConf: *remoteActionableInclude,
		}
		cfg := pkgreport.RemoteMatchConfig{
			TopN:         *remoteMatchTopN,
			Threshold:    *remoteMatchThreshold,
			LowThreshold: *remoteMatchLowThreshold,
		}
		options := auditOptions{writeReconcile: !*verifyFlag}
		result, err := runAudit(c, *itunesXml, filterOptions{
			album:  *filterAlbum,
			artist: *filterArtist,
			name:   *filterName,
			path:   *filterPath,
			limit:  *limitTracks,
		}, allowlist, selectedMatchMode, filterActive, *reportOnly, *runDir, *force, *failOnUnappliedLoved, paths, cfg, *remoteMatchDebug, options)
		if err != nil {
			log.Fatalf("Audit failed: %s", err)
		}
		if *verifyFlag {
			verifyCfg := verifyConfig{
				AllowUnstar: *allowUnstar,
				ConfigPath:  normalizeConfigPath(*configFile),
				PresetName:  *presetName,
				Thresholds: verifyThresholds{
					MaxStaleMissingStars:      *maxStaleMissingStars,
					MaxStaleMissingRatings:    *maxStaleMissingRatings,
					MaxStaleMissingPlaycounts: *maxStaleMissingPlays,
					InvalidLocationFatal:      *invalidLocationFatal,
					PlaylistInvalidFatal:      *playlistInvalidFatal,
				},
			}
			verifyReport := buildVerifyReport(result.Summary, verifyCfg)
			if err := writeVerifyArtifacts(result.Summary.Inputs.RunDir, verifyReport); err != nil {
				log.Fatalf("Failed to write verify artifacts: %s", err)
			}
			printVerifySummary(verifyReport)
			if !verifyReport.Go {
				os.Exit(2)
			}
		}
		return
	}

	if *reportRemoteStreaming != "" {
		if !*dryRun {
			log.Fatal("--report_remote_streaming_gaps is read-only; use --dry_run=true")
		}
		if err := runReportRemoteStreamingGaps(c, *itunesXml, *dumpFile, *reportRemoteStreaming); err != nil {
			log.Fatalf("Failed to write remote streaming gaps report: %s", err)
		}
		if *reportSyncPlan == "" && *reportReconcile == "" && *reportRemoteMatchJSON == "" && *reportRemoteMatchTSV == "" && *reportRemoteActionable == "" {
			return
		}
	}

	if *reportSyncPlan != "" || *reportReconcile != "" || *reportRemoteMatchJSON != "" || *reportRemoteMatchTSV != "" || *reportRemoteActionable != "" {
		filters := filterOptions{
			album:  *filterAlbum,
			artist: *filterArtist,
			name:   *filterName,
			path:   *filterPath,
			limit:  *limitTracks,
		}
		if *reportRemoteMatchJSON != "" || *reportRemoteMatchTSV != "" || *reportRemoteActionable != "" {
			if *reportSyncPlan == "" {
				log.Fatalf("--report_remote_match_* and --report_remote_actionable_tsv require --report_sync_plan (or use --audit)")
			}
			_, _, navidromeSongs, appleTracks, err := runReportSyncPlanWithData(c, *itunesXml, *reportSyncPlan, filters, allowlist, selectedMatchMode, filterActive, *reportOnly, *reportSyncPlanTSV)
			if err != nil {
				log.Fatalf("Failed to report sync plan: %s", err)
			}
			if *reportReconcile != "" {
				if err := runReportReconcile(*itunesXml, *reportSyncPlan, *reportReconcile, filters, *allowReconcileMismatch); err != nil {
					log.Fatalf("Failed to report reconcile summary: %s", err)
				}
			}
			paths := auditRemoteMatchPaths{
				jsonPath:       *reportRemoteMatchJSON,
				tsvPath:        *reportRemoteMatchTSV,
				actionableTSV:  *reportRemoteActionable,
				includeLowConf: *remoteActionableInclude,
			}
			cfg := pkgreport.RemoteMatchConfig{
				TopN:         *remoteMatchTopN,
				Threshold:    *remoteMatchThreshold,
				LowThreshold: *remoteMatchLowThreshold,
			}
			if _, err := runRemoteMatchReport(appleTracks, navidromeSongs, paths, cfg, *remoteMatchDebug); err != nil {
				log.Fatalf("Failed to write remote match report: %s", err)
			}
			if *dryRun {
				return
			}
		} else {
			if *reportSyncPlan != "" {
				if err := runReportSyncPlan(c, *itunesXml, *reportSyncPlan, filters, allowlist, selectedMatchMode, filterActive, *reportOnly); err != nil {
					log.Fatalf("Failed to report sync plan: %s", err)
				}
			}
			if *reportReconcile != "" {
				if err := runReportReconcile(*itunesXml, *reportSyncPlan, *reportReconcile, filters, *allowReconcileMismatch); err != nil {
					log.Fatalf("Failed to report reconcile summary: %s", err)
				}
			}
			if *dryRun {
				return
			}
		}
	}

	if !*dryRun {
		verifyReport, verifyPath, err := resolveVerifyReportForApply(*runDir, "run")
		if err != nil && !*force {
			log.Fatalf("Apply requires a prior --verify run (or use --force): %s", err)
		}
		if err == nil && !*force {
			normalizedConfig := normalizeConfigPath(*configFile)
			if verifyReport.ConfigPath != normalizedConfig || verifyReport.PresetName != *presetName {
				log.Fatalf("Apply requires the latest verify to use the same config/preset. Found %s (config=%q preset=%q)", verifyPath, verifyReport.ConfigPath, verifyReport.PresetName)
			}
			if !verifyReport.Go {
				log.Fatalf("Apply blocked because latest verify was NO-GO (%s). Re-run --verify or use --force.", verifyPath)
			}
		}
	}

	srcRelativePaths := buildRelativePathSet(srcSongs, *itunesRoot)

	if *probeSongID != "" {
		song, err := c.GetSong(*probeSongID)
		if err != nil {
			log.Fatalf("Failed to fetch song %q: %s", *probeSongID, err)
		}
		check := validateNavidromePath(song.Path, derivedMusicRoot, srcRelativePaths)
		fmt.Fprintf(stdoutWriter, "Subsonic client c=%q\n", *subsonicClient)
		fmt.Fprintf(stdoutWriter, "Probe song ID: %s\n", song.ID)
		fmt.Fprintf(stdoutWriter, "Raw path: %q\n", song.Path)
		fmt.Fprintf(stdoutWriter, "Decoded path: %q\n", check.decoded)
		fmt.Fprintf(stdoutWriter, "Clean path: %q\n", check.cleaned)
		if check.isAbsolute {
			fmt.Fprintln(stdoutWriter, "Path type: absolute")
		} else {
			fmt.Fprintln(stdoutWriter, "Path type: relative")
		}
		if check.isReal {
			fmt.Fprintln(stdoutWriter, "Real path validation: PASS")
		} else {
			fmt.Fprintf(stdoutWriter, "Real path validation: FAIL (%s)\n", check.reason)
		}
		return
	}

	if *probePath != "" {
		probeRoot := *subsonicRoot
		if probeRoot == "" {
			probeRoot = derivedMusicRoot
		}
		var (
			index       []navidromeIndexEntry
			indexSource string
		)
		if *dumpFile != "" {
			if _, err := os.Stat(*dumpFile); err == nil {
				entries, err := loadNavidromeDump(*dumpFile)
				if err != nil {
					log.Fatalf("Failed to read Navidrome dump %q: %s", *dumpFile, err)
				}
				index = buildNavidromeIndexFromDump(entries)
				indexSource = fmt.Sprintf("dump %s", *dumpFile)
			} else if !os.IsNotExist(err) {
				log.Fatalf("Failed to read Navidrome dump %q: %s", *dumpFile, err)
			}
		}
		if index == nil {
			fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
			dstSongs, err := fetchSubsonicSongs(c, fetchBar)
			if err != nil {
				log.Fatalf("Failed fetching navidrome songs: %s", err)
			}
			index = buildNavidromeIndexFromSongs(dstSongs, probeRoot, selectedMatchMode)
			indexSource = "live Navidrome index"
		}
		fmt.Fprintf(stdoutWriter, "Index source: %s\n", indexSource)
		probePathInIndex(*probePath, probeRoot, selectedMatchMode, index)
		return
	}

	fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
	dstSongs, err := fetchSubsonicSongs(c, fetchBar)
	if err != nil {
		log.Fatalf("Failed fetching navidrome songs: %s", err)
	}
	starredSongs, err := fetchStarredSongs(c)
	if err != nil {
		log.Fatalf("Failed fetching navidrome starred songs: %s", err)
	}
	starredByID := make(map[string]struct{}, len(starredSongs))
	for _, song := range starredSongs {
		starredByID[song.ID] = struct{}{}
	}
	for i := range dstSongs {
		if _, ok := starredByID[dstSongs[i].Id()]; ok {
			dstSongs[i].starred = true
		}
	}

	dstEligible := make([]subsonicInfo, 0, len(dstSongs))
	for _, song := range dstSongs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		ext, allowed := isExtensionAllowed(cleaned, allowlist)
		if !allowed {
			if ext == "" {
				ext = "<none>"
			}
			continue
		}
		dstEligible = append(dstEligible, song)
	}

	log.Printf("Src: total %d, eligible %d, remote %d, invalid_location %d, excluded_ext %d, stale_missing %d, path_mismatch %d",
		srcTotal, len(srcSongs), srcRemoteCount, srcInvalidLocationCount, srcExcludedExtensionCount, srcStaleMissingCount, srcPathMismatchCount)
	log.Printf("Dst: total %d, eligible %d", len(dstSongs), len(dstEligible))

	if *itunesRoot == "" && *subsonicRoot == "" && !filterActive {
		s := make([]i2s.SongInfo, 0, len(srcSongs))
		for _, si := range srcSongs {
			s = append(s, si)
		}
		d := make([]i2s.SongInfo, 0, len(dstEligible))
		for _, si := range dstEligible {
			d = append(d, si)
		}
		*itunesRoot, *subsonicRoot = i2s.LibraryPrefix(s, d)
	} else if *debugMode && filterActive && *itunesRoot == "" && *subsonicRoot == "" {
		log.Printf("Skipping auto library root detection because filters are active; matching full paths instead.")

	}
	if coerced, warned := coerceNonRootPath(*itunesRoot); warned {
		log.Printf("Warning: detected src library root was '/', treating as empty to avoid incorrect trimming.")
		*itunesRoot = coerced
	}
	if coerced, warned := coerceNonRootPath(*subsonicRoot); warned {
		log.Printf("Warning: detected dst library root was '/', treating as empty to avoid incorrect trimming.")
		*subsonicRoot = coerced
	}
	fmt.Fprintf(stdoutWriter, "Music library root: src='%s' dst='%s'\n", *itunesRoot, *subsonicRoot)

	srcRelativePaths = buildRelativePathSet(srcSongs, *itunesRoot)
	sampleCount := 200
	if sampleCount > len(dstSongs) {
		sampleCount = len(dstSongs)
	}
	var (
		realCount       int
		absoluteCount   int
		relativeCount   int
		suspiciousCount int
		examples        []string
	)
	for i := 0; i < sampleCount; i++ {
		song := dstSongs[i]
		check := validateNavidromePath(song.Path(), derivedMusicRoot, srcRelativePaths)
		if check.isAbsolute {
			absoluteCount++
		} else {
			relativeCount++
		}
		if check.isReal {
			realCount++
			continue
		}
		suspiciousCount++
		if len(examples) < 2 {
			examples = append(examples, fmt.Sprintf("id=%s raw=%q reason=%s", song.Id(), song.Path(), check.reason))
		}
	}
	if sampleCount > 0 {
		log.Printf("Subsonic client c=%q; sampled %d path(s): %d real (%d absolute, %d relative), %d suspicious.",
			*subsonicClient, sampleCount, realCount, absoluteCount, relativeCount, suspiciousCount)
	}
	threshold := 10
	if sampleCount > 0 {
		percentThreshold := int(0.05 * float64(sampleCount))
		if percentThreshold > threshold {
			threshold = percentThreshold
		}
	}
	if *requireRealPath && suspiciousCount > threshold {
		var exampleInfo string
		if len(examples) > 0 {
			exampleInfo = fmt.Sprintf(" Examples: %s.", strings.Join(examples, "; "))
		}
		log.Fatalf("Navidrome is returning virtual/tag-derived paths for Subsonic client c=%q. Enable “Report Real Path” for that player/client in Navidrome, choose a different --subsonic_client, or run with --require_real_path=false. You can also fall back to --match_mode=lenient.%s", *subsonicClient, exampleInfo)
	}
	if *dumpFile != "" {
		if err := writeNavidromeDump(*dumpFile, dstSongs, *subsonicRoot, selectedMatchMode); err != nil {
			log.Fatalf("Failed to write Navidrome dump %q: %s", *dumpFile, err)
		}
		log.Printf("Wrote Navidrome dump to %s", *dumpFile)
	}

	byPath := make(map[string]*songPair)
	byTrackID := make(map[int]*songPair)
	for _, s := range srcSongs {
		p := normalizeMatchPathWithMode(s.Path(), *itunesRoot, selectedMatchMode)
		t, ok := byPath[p]
		if !ok {
			t = &songPair{}
			byPath[p] = t
		}
		t.src = s
		byTrackID[s.id] = t
	}
	for _, s := range dstEligible {
		p := normalizeMatchPathWithMode(s.Path(), *subsonicRoot, selectedMatchMode)
		if filterActive {
			if _, ok := byPath[p]; !ok {
				continue
			}
		}
		t, ok := byPath[p]
		if !ok {
			t = &songPair{}
			byPath[p] = t
		}
		t.dst = s
	}

	plannedToStar, plannedToUnstar := buildStarUpdates(byPath)
	var plannedRatingSet int64
	var plannedRatingUnset int64
	for _, v := range byPath {
		if v.src.Id() == "" || v.dst.Id() == "" || v.src.FiveStarRating() == v.dst.FiveStarRating() {
			continue
		}
		if v.src.FiveStarRating() == 0 && !*copyUnrated {
			continue
		}
		if v.src.FiveStarRating() == 0 {
			plannedRatingUnset++
		} else {
			plannedRatingSet++
		}
	}
	var plannedPlayUpdates int64
	for _, v := range byPath {
		if v.src.Id() == "" || v.dst.Id() == "" {
			continue
		}
		if v.src.playCount == 0 && v.src.playDate.IsZero() {
			continue
		}
		if int64(v.src.playCount) <= v.dst.playCount && (v.src.playDate.IsZero() || v.dst.playCount > 0) {
			continue
		}
		plannedPlayUpdates++
	}
	plannedPlaylistCount := 0
	for _, playlist := range playlistRefs {
		if playlist.Master || playlist.Name == "" {
			continue
		}
		plannedPlaylistCount++
	}
	if !*dryRun {
		fmt.Fprintf(stdoutWriter, "About to write: stars=%d unstar=%d ratings_set=%d ratings_unset=%d playcount_updates=%d playlists=%d\n",
			len(plannedToStar),
			len(plannedToUnstar),
			plannedRatingSet,
			plannedRatingUnset,
			plannedPlayUpdates,
			plannedPlaylistCount,
		)
	}

	fmt.Fprintln(stdoutWriter, "== Missing Tracks ==")
	missingSrcCount := 0
	missingDstCount := 0
	for k, v := range byPath {
		if v.src.Id() != "" && v.dst.Id() != "" {
			continue
		}

		if v.src.Id() == "" && v.dst.Id() != "" {
			missingSrcCount++
			fmt.Fprintf(stdoutWriter, "%s\n\tmissing src(%s)\tdst(%s)\n", k, v.src.Id(), v.dst.Id())
		} else if v.dst.Id() == "" && v.src.Id() != "" {
			missingDstCount++
			entry := missingEntry{
				Side:     "dst_missing",
				Reason:   "not_in_navidrome_index",
				MatchKey: k,
				Src:      buildMissingSongFromSrc(v.src, *debugMode),
			}
			missingEntries = append(missingEntries, entry)
			appendMissingEntrySample(&notInNavidromeSamples, entry)
			if entry.Src != nil {
				ext := entry.Src.Extension
				if ext == "" {
					ext = "<none>"
				}
				notInNavidromeByExt[ext]++
				if prefix := directoryPrefixAfterRoot(entry.Src.Path, derivedMusicRoot, notInNavidromeDirSegments); prefix != "" {
					notInNavidromeByDir[prefix]++
				}
			}
			fmt.Fprintf(stdoutWriter, "%s\n\tmissing src(%s)\tdst(%s)\n", k, v.src.Id(), v.dst.Id())
		} else {
			fmt.Fprintf(stdoutWriter, "%s\n\tmissing src(%s)\tdst(%s)\n", k, v.src.Id(), v.dst.Id())
		}
	}
	fmt.Fprintln(stdoutWriter, "")
	missingEligibleCount := missingSrcCount + missingDstCount
	fmt.Fprintf(stdoutWriter, "== Missing Track Count %d / (%d + %d) ==\n", missingEligibleCount, len(srcSongs), len(dstEligible))

	if len(srcSongs)+len(dstEligible) > 0 && 100*missingEligibleCount/(len(srcSongs)+len(dstEligible)) > 90 {
		fmt.Fprintf(stdoutWriter, `Warning: Missing count is significant. Tips:
* Verify that the libraries are configured for the same directory
* Set --itunes_root and --subsonic_root to the correct values
* In Navidrome Player Settings, configure "Report Real Path"\n`)
	}
	if missingDstCount > 0 {
		fmt.Fprintln(stderrWriter, "Note: Some tracks exist in Apple Music as local files but were not returned by Navidrome’s Subsonic song list.")
		fmt.Fprintln(stderrWriter, "This usually means Navidrome has not indexed them, they are excluded by library config/scanner, or they are not readable by Navidrome.")
		fmt.Fprintln(stderrWriter, "Suggested checks:")
		fmt.Fprintln(stderrWriter, "  - confirm the file exists on disk at that path")
		fmt.Fprintln(stderrWriter, "  - confirm Navidrome has scanned/finished indexing")
		fmt.Fprintln(stderrWriter, "  - confirm the Navidrome process can read the file (permissions)")
		fmt.Fprintln(stderrWriter, "  - confirm the file type is supported by Navidrome/ffmpeg")
		fmt.Fprintln(stderrWriter, "  - confirm the file is under the configured MusicFolder (watch for multiple libraries or moved roots)")
		fmt.Fprintln(stderrWriter, "  - optionally use --probe_song_id or --probe_path to verify API visibility")
	}

	if *writeMissing != "" {
		if remoteSrcSamples == nil {
			remoteSrcSamples = []missingSong{}
		}
		if invalidSrcSamples == nil {
			invalidSrcSamples = []missingSong{}
		}
		if excludedExtensionSamples == nil {
			excludedExtensionSamples = []missingSong{}
		}
		if staleSrcSamples == nil {
			staleSrcSamples = []missingSong{}
		}
		if pathMismatchSamples == nil {
			pathMismatchSamples = []missingSong{}
		}
		if notInNavidromeSamples == nil {
			notInNavidromeSamples = []missingEntry{}
		}
		if notInNavidromeByExt == nil {
			notInNavidromeByExt = map[string]int{}
		}
		if notInNavidromeByDir == nil {
			notInNavidromeByDir = map[string]int{}
		}
		notInNavidromeByDir = limitCountsMap(notInNavidromeByDir, 10)
		report := missingReport{
			GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
			Version:         buildVersion(),
			MatchMode:       string(selectedMatchMode),
			RequireRealPath: *requireRealPath,
			SubsonicClient:  *subsonicClient,
			MusicRoot:       derivedMusicRoot,
			Extensions:      extensions,
			Stats: missingStats{
				SrcTotal:                  srcTotal,
				SrcEligible:               len(srcSongs),
				SrcRemoteCount:            srcRemoteCount,
				SrcInvalidLocationCount:   srcInvalidLocationCount,
				SrcExcludedExtensionCount: srcExcludedExtensionCount,
				SrcStaleMissingCount:      srcStaleMissingCount,
				SrcPathMismatchCount:      srcPathMismatchCount,
				DstTotal:                  len(dstSongs),
				DstEligible:               len(dstEligible),
			},
			Counts: missingCounts{
				MissingSrcCount:       missingSrcCount,
				MissingDstCount:       missingDstCount,
				PresentOnlyInDstCount: missingSrcCount,
			},
			Missing:                    missingEntries,
			SrcRemoteSamples:           remoteSrcSamples,
			SrcInvalidLocationSamples:  invalidSrcSamples,
			ExcludedExtensionSamples:   excludedExtensionSamples,
			StaleSrcFileSamples:        staleSrcSamples,
			SrcPathMismatchSamples:     pathMismatchSamples,
			NotInNavidromeIndexSamples: notInNavidromeSamples,
			NotInNavidromeIndexByExt:   notInNavidromeByExt,
			NotInNavidromeIndexByDir:   notInNavidromeByDir,
		}
		if err := writeMissingReport(*writeMissing, report); err != nil {
			log.Fatalf("Failed to write missing report %q: %s", *writeMissing, err)
		}
		log.Printf("Wrote missing report to %s", *writeMissing)
	}

	fmt.Fprintln(stdoutWriter, "== Mismatched Ratings ==")
	var mismatchCount int64 = 0
	for k, v := range byPath {
		if v.src.Id() == "" || v.dst.Id() == "" || v.src.FiveStarRating() == v.dst.FiveStarRating() {
			continue
		}
		if v.src.FiveStarRating() == 0 && !*copyUnrated {
			continue
		}

		fmt.Fprintf(stdoutWriter, "%s\n\trating src(%d)\tdst(%d)\n", k, v.src.FiveStarRating(), v.dst.FiveStarRating())
		mismatchCount++
	}
	fmt.Fprintln(stdoutWriter, "")

	fmt.Fprintf(stdoutWriter, "== Copy %d Ratings To Navidrome ==\n", mismatchCount)
	if *dryRun {
		fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s", *subsonicUrl)
	} else {
		fmt.Fprintf(stdoutWriter, "== Copy %d Ratings To Navidrome ==\n", mismatchCount)
		// Pause to give the user a chance to quit.
		time.Sleep(400 * time.Millisecond)

		skip := 0
		bar := i2s.PbWithOptions(pb.Default(mismatchCount, "set rating"))
		for k, v := range byPath {
			if v.src.Id() == "" || v.dst.Id() == "" || v.src.FiveStarRating() == v.dst.FiveStarRating() {
				continue
			}
			if v.src.FiveStarRating() == 0 && !*copyUnrated {
				continue
			}

			err := c.SetRating(v.dst.Id(), v.src.FiveStarRating())
			bar.Add(1)
			if err != nil {
				fmt.Fprintf(stderrWriter, "Error setting rating for '%s': %s\n", k, err)
				skip++
				if *skipCount > 0 && skip > *skipCount {
					log.Fatalf("Too many skipped tracks. Failing out...")
				}
			}
		}
		bar.Finish()
	}

	if *updatePlay {
		fmt.Fprintln(stdoutWriter, "== Play Count / Last Played ==")
		var playUpdates int64
		for _, v := range byPath {
			if v.src.Id() == "" || v.dst.Id() == "" {
				continue
			}
			if v.src.playCount == 0 && v.src.playDate.IsZero() {
				continue
			}
			if int64(v.src.playCount) <= v.dst.playCount && (v.src.playDate.IsZero() || v.dst.playCount > 0) {
				continue
			}
			playUpdates++
		}
		fmt.Fprintf(stdoutWriter, "== Sync %d Play Counts To Navidrome ==\n", playUpdates)
		if *dryRun {
			fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s\n", *subsonicUrl)
		} else {
			skip := 0
			bar := i2s.PbWithOptions(pb.Default(playUpdates, "scrobble plays"))
			for k, v := range byPath {
				if v.src.Id() == "" || v.dst.Id() == "" {
					continue
				}
				if v.src.playCount == 0 && v.src.playDate.IsZero() {
					continue
				}

				srcCount := int64(v.src.playCount)
				if srcCount <= v.dst.playCount && (v.src.playDate.IsZero() || v.dst.playCount > 0) {
					continue
				}

				desired := srcCount - v.dst.playCount
				if desired <= 0 {
					continue
				}
				if *maxScrobbles > 0 && desired > int64(*maxScrobbles) {
					desired = int64(*maxScrobbles)
				}

				scrobbleTime := time.Now().UTC()
				if !v.src.playDate.IsZero() {
					scrobbleTime = v.src.playDate.UTC()
				}

				for i := int64(0); i < desired; i++ {
					err := c.Scrobble(v.dst.Id(), map[string]string{
						"time":       strconv.FormatInt(scrobbleTime.Add(time.Duration(i)*time.Second).UnixMilli(), 10),
						"submission": "true",
					})
					if err != nil {
						fmt.Fprintf(stderrWriter, "Error setting play time for '%s': %s\n", k, err)
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
						break
					}
				}
				bar.Add(1)
			}
			bar.Finish()
		}
	}

	if *syncStarred {
		fmt.Fprintln(stdoutWriter, "== Favourited/Loved → Starred ==")
		toStar, _ := buildStarUpdates(byPath)
		var unstarCandidates []unstarCandidate
		var toUnstar []string
		unstarPath := filepath.Join(runDirFromLogFile(), "plan_unstar.tsv")
		if *syncUnstar {
			unstarCandidates = buildUnstarCandidates(byPath)
			if err := writeUnstarAuditTSV(unstarPath, unstarCandidates, selectedMatchMode); err != nil {
				log.Printf("Warning: failed to write %s: %s", unstarPath, err)
			}
			toUnstar = unstarIDs(unstarCandidates)
		}
		fmt.Fprintf(stdoutWriter, "== Sync %d Star / %d Unstar ==\n", len(toStar), len(toUnstar))
		if len(toUnstar) > 0 {
			fmt.Fprintf(stdoutWriter, "== Unstar Candidates (%d) ==\n", len(toUnstar))
			for _, entry := range unstarCandidates {
				appleID := "-"
				if entry.apple.id != 0 {
					appleID = strconv.Itoa(entry.apple.id)
				}
				fmt.Fprintf(stdoutWriter, "UNSTAR\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					entry.navidrome.id,
					entry.navidrome.artist,
					entry.navidrome.album,
					entry.navidrome.title,
					entry.navidrome.path,
					entry.reason,
					appleID,
				)
			}
		}
		if *dryRun {
			fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s\n", *subsonicUrl)
		} else {
			if len(toUnstar) > 0 && !*allowUnstar {
				log.Fatalf("Unstar operations requested (%d). Inspect %s and re-run with --allow_unstar=true to proceed.", len(toUnstar), unstarPath)
			}
			skip := 0
			if len(toStar) > 0 {
				bar := i2s.PbWithOptions(pb.Default(int64(len(toStar)), "star tracks"))
				for _, chunk := range chunkStrings(toStar, 200) {
					err := c.Star(subsonic.StarParameters{SongIDs: chunk})
					bar.Add(len(chunk))
					if err != nil {
						fmt.Fprintf(stderrWriter, "Error starring tracks: %s\n", err)
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
					}
				}
				bar.Finish()
			}
			if len(toUnstar) > 0 {
				bar := i2s.PbWithOptions(pb.Default(int64(len(toUnstar)), "unstar tracks"))
				for _, chunk := range chunkStrings(toUnstar, 200) {
					err := c.Unstar(subsonic.StarParameters{SongIDs: chunk})
					bar.Add(len(chunk))
					if err != nil {
						fmt.Fprintf(stderrWriter, "Error unstarring tracks: %s\n", err)
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
					}
				}
				bar.Finish()
			}
		}
	}

	if *syncPlaylist {
		fmt.Fprintln(stdoutWriter, "== Playlists ==")
		if len(playlistRefs) == 0 {
			fmt.Fprintln(stdoutWriter, "No playlists found in library.")
			return
		}
		existingPlaylists, err := c.GetPlaylists(nil)
		if err != nil {
			log.Fatalf("Failed fetching playlists: %s", err)
		}
		playlistsByName := make(map[string]*subsonic.Playlist)
		for _, playlist := range existingPlaylists {
			playlistsByName[playlist.Name] = playlist
		}

		var playlistCount int64
		for _, playlist := range playlistRefs {
			if playlist.Master || playlist.Name == "" {
				continue
			}
			playlistCount++
		}
		fmt.Fprintf(stdoutWriter, "== Sync %d Playlists ==\n", playlistCount)
		batchSize := *playlistBatchSize
		if batchSize <= 0 {
			batchSize = 250
		}
		failureDir := *runDir
		if failureDir == "" {
			failureDir = runDirFromLogFile()
		}
		failurePath := filepath.Join(failureDir, "playlist_failures.tsv")
		if *dryRun {
			fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s\n", *subsonicUrl)
			if err := os.MkdirAll(failureDir, 0o755); err != nil {
				log.Fatalf("Failed creating playlist failure directory: %s", err)
			}
			if err := writePlaylistFailuresTSV(failurePath, nil); err != nil {
				log.Fatalf("Failed writing playlist failures report: %s", err)
			}
		} else {
			skip := 0
			failures := make([]playlistFailure, 0)
			bar := i2s.PbWithOptions(pb.Default(playlistCount, "sync playlists"))
			for _, playlist := range playlistRefs {
				if playlist.Master || playlist.Name == "" {
					continue
				}

				var trackIDs []string
				for _, item := range playlist.Items {
					if item.TrackId == 0 {
						continue
					}
					if pair, ok := byTrackID[item.TrackId]; ok {
						if pair.dst.Id() != "" {
							trackIDs = append(trackIDs, pair.dst.Id())
						}
					}
				}

				if len(trackIDs) == 0 {
					bar.Add(1)
					continue
				}

				intendedOp := "create_and_add_batches"
				removes := 0
				if existing, ok := playlistsByName[playlist.Name]; ok {
					intendedOp = "recreate_and_add_batches"
					removes = int(existing.SongCount)
					if strings.TrimSpace(existing.ID) == "" {
						err := errMissingPlaylistID
						fmt.Fprintf(stderrWriter, "Error syncing playlist '%s': adds=%d removes=%d playlistId_available=false err=%s\n", playlist.Name, len(trackIDs), removes, err)
						failures = append(failures, playlistFailure{Name: playlist.Name, IntendedOp: intendedOp, Adds: len(trackIDs), Removes: removes, BatchSize: batchSize, Category: categorizePlaylistError(err), ErrorMessage: err.Error()})
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
						bar.Add(1)
						continue
					}
					err := withRetry(playlistRetryAttempts, 200*time.Millisecond, func() error {
						deleteParams := url.Values{}
						deleteParams.Add("playlistId", existing.ID)
						return subsonicRequest(c, "deletePlaylist", deleteParams)
					})
					if err != nil {
						fmt.Fprintf(stderrWriter, "Error recreating playlist '%s': adds=%d removes=%d playlistId_available=true err=%s\n", playlist.Name, len(trackIDs), removes, err)
						failures = append(failures, playlistFailure{Name: playlist.Name, IntendedOp: intendedOp, Adds: len(trackIDs), Removes: removes, BatchSize: batchSize, Category: categorizePlaylistError(err), ErrorMessage: err.Error()})
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
						bar.Add(1)
						continue
					}
				}

				playlistID, err := ensurePlaylistID(c, playlist.Name)
				if err != nil || strings.TrimSpace(playlistID) == "" {
					if err == nil {
						err = errors.New("playlist_not_found")
					}
					fmt.Fprintf(stderrWriter, "Error ensuring playlist '%s': adds=%d removes=%d playlistId_available=false err=%s\n", playlist.Name, len(trackIDs), removes, err)
					failures = append(failures, playlistFailure{Name: playlist.Name, IntendedOp: intendedOp, Adds: len(trackIDs), Removes: removes, BatchSize: batchSize, Category: categorizePlaylistError(err), ErrorMessage: err.Error()})
					skip++
					if *skipCount > 0 && skip > *skipCount {
						log.Fatalf("Too many skipped tracks. Failing out...")
					}
					bar.Add(1)
					continue
				}

				err = updatePlaylistBatched(func(endpoint string, params url.Values) error {
					return withRetry(playlistRetryAttempts, 200*time.Millisecond, func() error {
						return subsonicRequest(c, endpoint, params)
					})
				}, playlistID, trackIDs, batchSize)
				if err != nil {
					fmt.Fprintf(stderrWriter, "Error updating playlist '%s': adds=%d removes=%d playlistId_available=true err=%s\n", playlist.Name, len(trackIDs), removes, err)
					failures = append(failures, playlistFailure{Name: playlist.Name, IntendedOp: intendedOp, Adds: len(trackIDs), Removes: removes, BatchSize: batchSize, Category: categorizePlaylistError(err), ErrorMessage: err.Error()})
					skip++
					if *skipCount > 0 && skip > *skipCount {
						log.Fatalf("Too many skipped tracks. Failing out...")
					}
					bar.Add(1)
					continue
				}
				bar.Add(1)
			}
			bar.Finish()
			if err := os.MkdirAll(failureDir, 0o755); err != nil {
				log.Fatalf("Failed creating playlist failure directory: %s", err)
			}
			if err := writePlaylistFailuresTSV(failurePath, failures); err != nil {
				log.Fatalf("Failed writing playlist failures report: %s", err)
			}
		}
	}
	//
	//	if *createdFile != "" {
	//		f, err := os.OpenFile(*createdFile, os.O_RDWR|os.O_CREATE, 0644)
	//		if err != nil {
	//			log.Fatalf("Failed to open given play file: %s", err)
	//		}
	//		defer f.Close()
	//
	//		err = writeCreatedSql(f, tracks)
	//		if err != nil {
	//			log.Fatalf("Failed to write play file: %s", err)
	//		}
	//	}
}
