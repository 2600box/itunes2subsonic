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
	"strconv"
	"strings"
	"time"

	"github.com/delucks/go-subsonic"
	i2s "github.com/logank/itunes2subsonic"
	"github.com/logank/itunes2subsonic/internal/itunes"
	pb "github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

var (
	dryRun       = flag.Bool("dry_run", true, "don't modify the library")
	itunesXml    = flag.String("itunes_xml", "Apple Music Library.xml", "path to the Apple Music Library XML to import")
	skipCount    = flag.Int("skip_count", 10, "a limit on the number of tracks that would be skipped before refusing to process")
	copyUnrated  = flag.Bool("copy_unrated", false, "if true, will unset rating if src is unrated")
	subsonicUrl  = flag.String("subsonic", "", "url of the Navidrome instance")
	updatePlay   = flag.Bool("update_played", true, "update play count and last played time")
	syncStarred  = flag.Bool("sync_starred", true, "sync Apple Music loved tracks to Navidrome starred")
	syncPlaylist = flag.Bool("sync_playlists", true, "sync Apple Music playlists to Navidrome")
	maxScrobbles = flag.Int("max_scrobbles", 250, "maximum scrobbles per track when syncing play counts")
	createdFile  = flag.String("created_file", "", "a file to write SQL statements to update the created time")
	itunesRoot   = flag.String("itunes_root", "", "(optional) library prefix for Apple Music content")
	subsonicRoot = flag.String("subsonic_root", "", "(optional) library prefix for Navidrome content")
	filterAlbum  = flag.String("filter_album", "", "only sync tracks whose album contains this text")
	filterArtist = flag.String("filter_artist", "", "only sync tracks whose artist contains this text")
	filterName   = flag.String("filter_name", "", "only sync tracks whose title contains this text")
	filterPath   = flag.String("filter_path", "", "only sync tracks whose path contains this text")
	limitTracks  = flag.Int("limit_tracks", 0, "only sync the first N matching tracks (0 means no limit)")
	debugMode    = flag.Bool("debug", false, "enable debug logging for filtering and matching")
	logFile      = flag.String("log_file", "", "write logs to the specified file (defaults to stderr only)")
	dumpFile     = flag.String("navidrome_dump", "", "write Navidrome track metadata (including raw paths) to a JSON file")
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
	fmt.Printf("%s: ", label)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func promptPassword(label string) (string, error) {
	fmt.Printf("%s: ", label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println("")
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

func writeNavidromeDump(path string, songs []subsonicInfo, root string) error {
	type dumpEntry struct {
		ID        string `json:"id"`
		Path      string `json:"path"`
		CleanPath string `json:"clean_path"`
		MatchPath string `json:"match_path"`
	}
	entries := make([]dumpEntry, 0, len(songs))
	for _, song := range songs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		entries = append(entries, dumpEntry{
			ID:        song.Id(),
			Path:      song.Path(),
			CleanPath: cleaned,
			MatchPath: normalizeMatchPath(song.Path(), root),
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
	decoded := safePathUnescape(pathValue)
	normalized := filepath.Clean(filepath.FromSlash(decoded))
	rootDecoded := safePathUnescape(root)
	rootNormalized := filepath.Clean(filepath.FromSlash(rootDecoded))
	if rootNormalized == "." || rootNormalized == string(os.PathSeparator) {
		rootNormalized = ""
	}
	normalized = strings.TrimLeft(normalized, string(os.PathSeparator))
	rootNormalized = strings.TrimLeft(rootNormalized, string(os.PathSeparator))
	return strings.TrimPrefix(strings.ToLower(normalized), strings.ToLower(rootNormalized))
}

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
	var logFileHandle *os.File
	if *logFile != "" {
		handle, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			log.Fatalf("Failed to open log file %q: %s", *logFile, err)
		}
		logFileHandle = handle
		log.SetOutput(io.MultiWriter(os.Stderr, logFileHandle))
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
			})
		}
	}

	c := &subsonic.Client{
		Client:     &http.Client{},
		BaseUrl:    *subsonicUrl,
		User:       subsonicUser,
		ClientName: "apple-music2navidrome",
	}
	if err := c.Authenticate(subsonicPass); err != nil {
		log.Fatalf("Failed to create Navidrome client: %s", err)
	}

	if *debugMode {
		log.Printf("Filters: album=%q artist=%q name=%q path=%q limit=%d", *filterAlbum, *filterArtist, *filterName, *filterPath, *limitTracks)
	}

	fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
	dstSongs, err := fetchSubsonicSongs(c, fetchBar)
	if err != nil {
		log.Fatalf("Failed fetching navidrome songs: %s", err)
	}

	log.Printf("Src track count %d, Dst track count %d\n", len(srcSongs), len(dstSongs))

	filterActive := *filterAlbum != "" || *filterArtist != "" || *filterName != "" || *filterPath != "" || *limitTracks > 0
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
	fmt.Printf("Music library root: src='%s' dst='%s'\n", *itunesRoot, *subsonicRoot)
	if *dumpFile != "" {
		if err := writeNavidromeDump(*dumpFile, dstSongs, *subsonicRoot); err != nil {
			log.Fatalf("Failed to write Navidrome dump %q: %s", *dumpFile, err)
		}
		log.Printf("Wrote Navidrome dump to %s", *dumpFile)
	}

	byPath := make(map[string]*songPair)
	byTrackID := make(map[int]*songPair)
	for _, s := range srcSongs {
		p := normalizeMatchPath(s.Path(), *itunesRoot)
		t, ok := byPath[p]
		if !ok {
			t = &songPair{}
			byPath[p] = t
		}
		t.src = s
		byTrackID[s.id] = t
	}
	for _, s := range dstSongs {
		p := normalizeMatchPath(s.Path(), *subsonicRoot)
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

	fmt.Println("== Missing Tracks ==")
	missingCount := 0
	for k, v := range byPath {
		if v.src.Id() != "" && v.dst.Id() != "" {
			continue
		}

		missingCount++
		fmt.Printf("%s\n\tmissing src(%s)\tdst(%s)\n", k, v.src.Id(), v.dst.Id())
	}
	fmt.Println("")
	fmt.Printf("== Missing Track Count %d / (%d + %d) ==\n", missingCount, len(srcSongs), len(dstSongs))

	if 100*missingCount/(len(srcSongs)+len(dstSongs)) > 90 {
		fmt.Printf(`Warning: Missing count is significant. Tips:
* Verify that the libraries are configured for the same directory
* Set --itunes_root and --subsonic_root to the correct values
* In Navidrome Player Settings, configure "Report Real Path"\n`)
	}

	fmt.Println("== Mismatched Ratings ==")
	var mismatchCount int64 = 0
	for k, v := range byPath {
		if v.src.Id() == "" || v.dst.Id() == "" || v.src.FiveStarRating() == v.dst.FiveStarRating() {
			continue
		}
		if v.src.FiveStarRating() == 0 && !*copyUnrated {
			continue
		}

		fmt.Printf("%s\n\trating src(%d)\tdst(%d)\n", k, v.src.FiveStarRating(), v.dst.FiveStarRating())
		mismatchCount++
	}
	fmt.Println("")

	fmt.Printf("== Copy %d Ratings To Navidrome ==\n", mismatchCount)
	if *dryRun {
		fmt.Printf("Set --dry_run=false to modify %s", *subsonicUrl)
	} else {
		fmt.Printf("== Copy %d Ratings To Navidrome ==\n", mismatchCount)
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
				fmt.Fprintf(os.Stderr, "Error setting rating for '%s': %s\n", k, err)
				skip++
				if *skipCount > 0 && skip > *skipCount {
					log.Fatalf("Too many skipped tracks. Failing out...")
				}
			}
		}
		bar.Finish()
	}

	if *updatePlay {
		fmt.Println("== Play Count / Last Played ==")
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
		fmt.Printf("== Sync %d Play Counts To Navidrome ==\n", playUpdates)
		if *dryRun {
			fmt.Printf("Set --dry_run=false to modify %s\n", *subsonicUrl)
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
						fmt.Fprintf(os.Stderr, "Error setting play time for '%s': %s\n", k, err)
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
		fmt.Println("== Loved/Starred ==")
		var toStar []string
		var toUnstar []string
		for _, v := range byPath {
			if v.src.Id() == "" || v.dst.Id() == "" {
				continue
			}
			if v.src.loved && !v.dst.starred {
				toStar = append(toStar, v.dst.Id())
			}
			if !v.src.loved && v.dst.starred {
				toUnstar = append(toUnstar, v.dst.Id())
			}
		}
		fmt.Printf("== Sync %d Star / %d Unstar ==\n", len(toStar), len(toUnstar))
		if *dryRun {
			fmt.Printf("Set --dry_run=false to modify %s\n", *subsonicUrl)
		} else {
			skip := 0
			if len(toStar) > 0 {
				bar := i2s.PbWithOptions(pb.Default(int64(len(toStar)), "star tracks"))
				for _, chunk := range chunkStrings(toStar, 200) {
					err := c.Star(subsonic.StarParameters{SongIDs: chunk})
					bar.Add(len(chunk))
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error starring tracks: %s\n", err)
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
						fmt.Fprintf(os.Stderr, "Error unstarring tracks: %s\n", err)
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
		fmt.Println("== Playlists ==")
		if len(playlistRefs) == 0 {
			fmt.Println("No playlists found in library.")
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
		fmt.Printf("== Sync %d Playlists ==\n", playlistCount)
		if *dryRun {
			fmt.Printf("Set --dry_run=false to modify %s\n", *subsonicUrl)
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
							fmt.Fprintf(os.Stderr, "Error clearing playlist '%s': %s\n", playlist.Name, err)
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
						fmt.Fprintf(os.Stderr, "Error updating playlist '%s': %s\n", playlist.Name, err)
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
						fmt.Fprintf(os.Stderr, "Error creating playlist '%s': %s\n", playlist.Name, err)
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
