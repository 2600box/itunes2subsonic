package main

// Notes:
// -   Normalizes paths to lower case because Apple Music/Windows doesn't update if the underlying file changes.
// -   Navidrome requires going into the Player settings and configuring "Report Real Path"

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/delucks/go-subsonic"
	i2s "github.com/logank/itunes2subsonic"
	"github.com/logank/itunes2subsonic/internal/itunes"
	pb "github.com/schollz/progressbar/v3"
	"golang.org/x/term"
	"golang.org/x/text/unicode/norm"
)

var (
	dryRun          = flag.Bool("dry_run", true, "don't modify the library")
	itunesXml       = flag.String("itunes_xml", "Apple Music Library.xml", "path to the Apple Music Library XML to import")
	skipCount       = flag.Int("skip_count", 10, "a limit on the number of tracks that would be skipped before refusing to process")
	copyUnrated     = flag.Bool("copy_unrated", false, "if true, will unset rating if src is unrated")
	subsonicUrl     = flag.String("subsonic", "", "url of the Navidrome instance")
	updatePlay      = flag.Bool("update_played", true, "update play count and last played time")
	syncStarred     = flag.Bool("sync_starred", true, "sync Apple Music loved tracks to Navidrome starred")
	syncPlaylist    = flag.Bool("sync_playlists", true, "sync Apple Music playlists to Navidrome")
	maxScrobbles    = flag.Int("max_scrobbles", 250, "maximum scrobbles per track when syncing play counts")
	createdFile     = flag.String("created_file", "", "a file to write SQL statements to update the created time")
	itunesRoot      = flag.String("itunes_root", "", "(optional) library prefix for Apple Music content")
	subsonicRoot    = flag.String("subsonic_root", "", "(optional) library prefix for Navidrome content")
	musicRoot       = flag.String("music_root", "", "(optional) root of the on-disk music folder for real-path checks")
	filterAlbum     = flag.String("filter_album", "", "only sync tracks whose album contains this text")
	filterArtist    = flag.String("filter_artist", "", "only sync tracks whose artist contains this text")
	filterName      = flag.String("filter_name", "", "only sync tracks whose title contains this text")
	filterPath      = flag.String("filter_path", "", "only sync tracks whose path contains this text")
	limitTracks     = flag.Int("limit_tracks", 0, "only sync the first N matching tracks (0 means no limit)")
	debugMode       = flag.Bool("debug", false, "enable debug logging for filtering and matching")
	logFile         = flag.String("log_file", "", "write logs to the specified file (defaults to stderr only)")
	dumpFile        = flag.String("navidrome_dump", "", "write Navidrome track metadata (including raw paths) to a JSON file")
	writeMissing    = flag.String("write_missing", "", "write missing track metadata to a JSON file")
	subsonicClient  = flag.String("subsonic_client", "itunes2subsonic", "Subsonic client identifier (c=) to use when connecting")
	requireRealPath = flag.Bool("require_real_path", true, "fail fast if Navidrome returns virtual/tag paths instead of real paths")
	matchMode       = flag.String("match_mode", "realpath", "path matching mode: realpath or lenient")
	probeSongID     = flag.String("probe_song_id", "", "if set, fetch /rest/getSong for the given ID and validate its path")
)

var (
	stdoutWriter io.Writer = os.Stdout
	stderrWriter io.Writer = os.Stderr
)

type subsonicInfo struct {
	id        string
	path      string
	rating    int
	playCount int64
	starred   bool
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
}

func (s itunesInfo) Id() string          { return strconv.Itoa(s.id) }
func (s itunesInfo) Path() string        { return s.path }
func (s itunesInfo) FiveStarRating() int { return s.rating / 20 }

type songPair struct {
	src itunesInfo
	dst subsonicInfo
}

type playlistRef struct {
	Name   string
	Master bool
	Items  []itunes.PlaylistItem
}

type missingCounts struct {
	SrcTotal   int `json:"src_total"`
	DstTotal   int `json:"dst_total"`
	MissingSrc int `json:"missing_src"`
	MissingDst int `json:"missing_dst"`
}

type missingSong struct {
	ID          string `json:"id,omitempty"`
	Path        string `json:"path,omitempty"`
	Name        string `json:"name,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	RawPath     string `json:"raw_path,omitempty"`
	DecodedPath string `json:"decoded_path,omitempty"`
	CleanPath   string `json:"clean_path,omitempty"`
}

type missingEntry struct {
	Side     string       `json:"side"`
	MatchKey string       `json:"match_key"`
	Src      *missingSong `json:"src,omitempty"`
	Dst      *missingSong `json:"dst,omitempty"`
}

type missingReport struct {
	GeneratedAt     string         `json:"generated_at"`
	MatchMode       string         `json:"match_mode"`
	RequireRealPath bool           `json:"require_real_path"`
	MusicRoot       string         `json:"music_root"`
	Counts          missingCounts  `json:"counts"`
	Missing         []missingEntry `json:"missing"`
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
				id:        s.ID,
				path:      s.Path,
				rating:    s.UserRating,
				playCount: s.PlayCount,
				starred:   !s.Starred.IsZero(),
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

func writeNavidromeDump(path string, songs []subsonicInfo, root string, mode matchModeValue) error {
	type dumpEntry struct {
		ID          string `json:"id"`
		Path        string `json:"path"`
		RawPath     string `json:"raw_path"`
		DecodedPath string `json:"decoded_path"`
		CleanPath   string `json:"clean_path"`
		MatchPath   string `json:"match_path"`
	}
	entries := make([]dumpEntry, 0, len(songs))
	for _, song := range songs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		matchPath := normalizeMatchPathWithMode(song.Path(), root, mode)
		if *debugMode && song.Id() == "10c87dea0ab488cb39f7f607ea8c0f0d" {
			log.Printf("Navidrome dump debug for %s: raw=%q decoded=%q clean=%q normalised=%q", song.Id(), song.Path(), decoded, cleaned, matchPath)
		}
		entries = append(entries, dumpEntry{
			ID:          song.Id(),
			Path:        song.Path(),
			RawPath:     song.Path(),
			DecodedPath: decoded,
			CleanPath:   cleaned,
			MatchPath:   matchPath,
		})
	}
	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func subsonicRequest(c *subsonic.Client, endpoint string, params url.Values) error {
	resp, err := c.Request("GET", endpoint, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	parsed := subsonic.Response{}
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return err
	}
	if parsed.Error != nil {
		return fmt.Errorf("Error #%d: %s", parsed.Error.Code, parsed.Error.Message)
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

func normalizeLocation(loc string) (string, error) {
	if loc == "" {
		return "", nil
	}
	parsed, err := url.Parse(loc)
	if err == nil && parsed.Scheme != "" {
		pathValue := parsed.Path
		if pathValue == "" {
			pathValue = parsed.Opaque
		}
		decoded := safePathUnescape(pathValue)
		return filepath.Clean(filepath.FromSlash(decoded)), nil
	}

	decoded := safePathUnescape(loc)
	return filepath.Clean(filepath.FromSlash(decoded)), nil
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
	normalized := normalizeRootPath(root)
	if normalized == "" {
		return ""
	}
	if looksLikeAudioFile(normalized) {
		return normalizeRootPath(filepath.Dir(normalized))
	}
	info, err := os.Stat(normalized)
	if err == nil && !info.IsDir() {
		return normalizeRootPath(filepath.Dir(normalized))
	}
	return normalized
}

func looksLikeAudioFile(pathValue string) bool {
	switch strings.ToLower(filepath.Ext(pathValue)) {
	case ".mp3", ".m4a", ".flac", ".ogg", ".opus", ".wav", ".aiff", ".mp4":
		return true
	default:
		return false
	}
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
		srcLoved := v.src.loved || v.src.favorited
		if srcLoved && !v.dst.starred {
			toStar = append(toStar, v.dst.Id())
		}
		if !srcLoved && v.dst.starred {
			toUnstar = append(toUnstar, v.dst.Id())
		}
	}
	return toStar, toUnstar
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
	entry := &missingSong{
		ID:     src.Id(),
		Path:   src.Path(),
		Name:   src.name,
		Artist: src.artist,
		Album:  src.album,
	}
	if includeDebug {
		decoded := safePathUnescape(src.Path())
		entry.RawPath = src.Path()
		entry.DecodedPath = decoded
		entry.CleanPath = filepath.Clean(filepath.FromSlash(decoded))
	}
	return entry
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
	filterActive := *filterAlbum != "" || *filterArtist != "" || *filterName != "" || *filterPath != "" || *limitTracks > 0
	var logFileHandle *os.File
	if *logFile != "" {
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
	cfg := loadConfig()
	subsonicUser := firstNonEmpty(os.Getenv("SUBSONIC_USER"), cfg.SubsonicUser)
	subsonicPass := firstNonEmpty(os.Getenv("SUBSONIC_PASS"), cfg.SubsonicPass)
	if *subsonicUrl == "" {
		*subsonicUrl = cfg.SubsonicURL
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

	cfg.SubsonicURL = *subsonicUrl
	cfg.SubsonicUser = subsonicUser
	cfg.SubsonicPass = subsonicPass
	if err := saveConfig(cfg); err != nil {
		log.Printf("Warning: failed to save config: %s", err)
	}

	var srcSongs []itunesInfo
	var playlistRefs []playlistRef
	var matchedCount int
	if *itunesXml != "" {
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
			loc, err := normalizeLocation(v.Location)
			if err != nil {
				log.Fatalf("Unexpected Apple Music location '%s': %s", v.Location, err)
			}

			if !matchesFilter(v.Album, *filterAlbum) || !matchesFilter(v.Artist, *filterArtist) || !matchesFilter(v.Name, *filterName) || !matchesFilter(loc, *filterPath) {
				continue
			}
			if *limitTracks > 0 && matchedCount >= *limitTracks {
				break
			}
			matchedCount++

			srcSongs = append(srcSongs, itunesInfo{
				id:        v.TrackId,
				path:      loc,
				name:      v.Name,
				artist:    v.Artist,
				album:     v.Album,
				rating:    v.Rating,
				playDate:  v.PlayDateUTC,
				dateAdded: v.DateAdded,
				playCount: v.PlayCount,
				loved:     v.Loved,
				favorited: v.Favorited,
			})
		}
	}

	var derivedMusicRoot string
	if *musicRoot != "" {
		derivedMusicRoot = normalizeMusicRootPath(*musicRoot)
	} else if filterActive {
		derivedMusicRoot = ""
		if *debugMode {
			log.Printf("Skipping music_root derivation because filters are active and --music_root was not set.")
		}
	} else if *itunesRoot != "" {
		derivedMusicRoot = normalizeMusicRootPath(*itunesRoot)
	} else {
		paths := make([]string, 0, len(srcSongs))
		for _, song := range srcSongs {
			if song.Path() == "" {
				continue
			}
			paths = append(paths, song.Path())
		}
		derivedMusicRoot = normalizeMusicRootPath(deriveMusicRoot(paths))
	}
	if coerced, warned := coerceNonRootPath(derivedMusicRoot); warned {
		log.Printf("Warning: detected music root was '/', treating as unknown to avoid incorrect trimming.")
		derivedMusicRoot = coerced
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
		log.Printf("Subsonic client: c=%q", *subsonicClient)
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

	fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
	dstSongs, err := fetchSubsonicSongs(c, fetchBar)
	if err != nil {
		log.Fatalf("Failed fetching navidrome songs: %s", err)
	}

	log.Printf("Src track count %d, Dst track count %d\n", len(srcSongs), len(dstSongs))

	if *itunesRoot == "" && *subsonicRoot == "" && !filterActive {
		s := make([]i2s.SongInfo, 0, len(srcSongs))
		for _, si := range srcSongs {
			s = append(s, si)
		}
		d := make([]i2s.SongInfo, 0, len(dstSongs))
		for _, si := range dstSongs {
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
	for _, s := range dstSongs {
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

	fmt.Fprintln(stdoutWriter, "== Missing Tracks ==")
	missingCount := 0
	missingSrcCount := 0
	missingDstCount := 0
	missingEntries := make([]missingEntry, 0)
	for k, v := range byPath {
		if v.src.Id() != "" && v.dst.Id() != "" {
			continue
		}

		missingCount++
		entry := missingEntry{
			MatchKey: k,
		}
		if v.src.Id() == "" && v.dst.Id() != "" {
			missingSrcCount++
			entry.Side = "src_missing"
			entry.Dst = buildMissingSongFromDst(v.dst, *debugMode)
		} else if v.dst.Id() == "" && v.src.Id() != "" {
			missingDstCount++
			entry.Side = "dst_missing"
			entry.Src = buildMissingSongFromSrc(v.src, *debugMode)
		} else {
			entry.Side = "unknown_missing"
			entry.Src = buildMissingSongFromSrc(v.src, *debugMode)
			entry.Dst = buildMissingSongFromDst(v.dst, *debugMode)
		}
		missingEntries = append(missingEntries, entry)
		fmt.Fprintf(stdoutWriter, "%s\n\tmissing src(%s)\tdst(%s)\n", k, v.src.Id(), v.dst.Id())
	}
	fmt.Fprintln(stdoutWriter, "")
	fmt.Fprintf(stdoutWriter, "== Missing Track Count %d / (%d + %d) ==\n", missingCount, len(srcSongs), len(dstSongs))

	if 100*missingCount/(len(srcSongs)+len(dstSongs)) > 90 {
		fmt.Fprintf(stdoutWriter, `Warning: Missing count is significant. Tips:
* Verify that the libraries are configured for the same directory
* Set --itunes_root and --subsonic_root to the correct values
* In Navidrome Player Settings, configure "Report Real Path"\n`)
	}

	if *writeMissing != "" {
		report := missingReport{
			GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
			MatchMode:       string(selectedMatchMode),
			RequireRealPath: *requireRealPath,
			MusicRoot:       derivedMusicRoot,
			Counts: missingCounts{
				SrcTotal:   len(srcSongs),
				DstTotal:   len(dstSongs),
				MissingSrc: missingSrcCount,
				MissingDst: missingDstCount,
			},
			Missing: missingEntries,
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
		fmt.Fprintln(stdoutWriter, "== Loved/Starred ==")
		toStar, toUnstar := buildStarUpdates(byPath)
		fmt.Fprintf(stdoutWriter, "== Sync %d Star / %d Unstar ==\n", len(toStar), len(toUnstar))
		if *dryRun {
			fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s\n", *subsonicUrl)
		} else {
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
		if *dryRun {
			fmt.Fprintf(stdoutWriter, "Set --dry_run=false to modify %s\n", *subsonicUrl)
		} else {
			skip := 0
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

				if existing, ok := playlistsByName[playlist.Name]; ok {
					removeParams := url.Values{}
					for i := 0; i < int(existing.SongCount); i++ {
						removeParams.Add("songIndexToRemove", strconv.Itoa(i))
					}
					if len(removeParams) > 0 {
						if err := subsonicRequest(c, "updatePlaylist", removeParams); err != nil {
							fmt.Fprintf(stderrWriter, "Error clearing playlist '%s': %s\n", playlist.Name, err)
							skip++
							if *skipCount > 0 && skip > *skipCount {
								log.Fatalf("Too many skipped tracks. Failing out...")
							}
							bar.Add(1)
							continue
						}
					}
					addParams := url.Values{}
					addParams.Add("playlistId", existing.ID)
					for _, id := range trackIDs {
						addParams.Add("songIdToAdd", id)
					}
					if err := subsonicRequest(c, "updatePlaylist", addParams); err != nil {
						fmt.Fprintf(stderrWriter, "Error updating playlist '%s': %s\n", playlist.Name, err)
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
					}
				} else {
					createParams := url.Values{}
					createParams.Add("name", playlist.Name)
					for _, id := range trackIDs {
						createParams.Add("songId", id)
					}
					if err := subsonicRequest(c, "createPlaylist", createParams); err != nil {
						fmt.Fprintf(stderrWriter, "Error creating playlist '%s': %s\n", playlist.Name, err)
						skip++
						if *skipCount > 0 && skip > *skipCount {
							log.Fatalf("Too many skipped tracks. Failing out...")
						}
					}
				}
				bar.Add(1)
			}
			bar.Finish()
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
