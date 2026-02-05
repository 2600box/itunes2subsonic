package main

// Notes:
// -   Normalizes paths to lower case because Apple Music/Windows doesn't update if the underlying file changes.
// -   Navidrome requires going into the Player settings and configuring "Report Real Path"

import (
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
)

var (
	dryRun       = flag.Bool("dry_run", true, "don't modify the library")
	itunesXml    = flag.String("itunes_xml", "Apple Music Library.xml", "path to the Apple Music Library XML to import")
	skipCount    = flag.Int("skip_count", 10, "a limit on the number of tracks that would be skipped before refusing to process")
	copyUnrated  = flag.Bool("copy_unrated", false, "if true, will unset rating if src is unrated")
	subsonicUrl  = flag.String("subsonic", "", "url of the Subsonic instance")
	updatePlay   = flag.Bool("update_played", true, "update play count and last played time")
	syncStarred  = flag.Bool("sync_starred", true, "sync Apple Music loved tracks to Subsonic starred")
	syncPlaylist = flag.Bool("sync_playlists", true, "sync Apple Music playlists to Subsonic")
	maxScrobbles = flag.Int("max_scrobbles", 250, "maximum scrobbles per track when syncing play counts")
	createdFile  = flag.String("created_file", "", "a file to write SQL statements to update the created time")
	itunesRoot   = flag.String("itunes_root", "", "(optional) library prefix for Apple Music content")
	subsonicRoot = flag.String("subsonic_root", "", "(optional) library prefix for Subsonic content")
	filterAlbum  = flag.String("filter_album", "", "only sync tracks whose album contains this text")
	filterArtist = flag.String("filter_artist", "", "only sync tracks whose artist contains this text")
	filterName   = flag.String("filter_name", "", "only sync tracks whose title contains this text")
	filterPath   = flag.String("filter_path", "", "only sync tracks whose path contains this text")
	limitTracks  = flag.Int("limit_tracks", 0, "only sync the first N matching tracks (0 means no limit)")
	debugMode    = flag.Bool("debug", false, "enable debug logging for filtering and matching")
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
			log.Fatalf("Failed fetching Subsonic songs: %s", err)
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
		decoded, err := url.PathUnescape(pathValue)
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.FromSlash(decoded)), nil
	}

	decoded, err := url.PathUnescape(loc)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.FromSlash(decoded)), nil
}

func normalizeMatchPath(pathValue string, root string) string {
	decoded, err := url.PathUnescape(pathValue)
	if err != nil {
		decoded = pathValue
	}
	normalized := filepath.Clean(filepath.FromSlash(decoded))
	rootDecoded, err := url.PathUnescape(root)
	if err != nil {
		rootDecoded = root
	}
	rootNormalized := filepath.Clean(filepath.FromSlash(rootDecoded))
	if rootNormalized == "." {
		rootNormalized = ""
	}
	return strings.TrimPrefix(strings.ToLower(normalized), strings.ToLower(rootNormalized))
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
	subsonicUser, subsonicPass := os.Getenv("SUBSONIC_USER"), os.Getenv("SUBSONIC_PASS")

	if (subsonicUser != "" || *subsonicUrl != "") && subsonicPass == "" {
		log.Fatal("If connecting to Subsonic, you must set the SUBSONIC_USER and SUBSONIC_PASS environment variables.")
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
		Client:         &http.Client{},
		BaseUrl:        *subsonicUrl,
		User:           subsonicUser,
		ClientName:     "apple-music2subsonic",
		RequireDotView: true,
	}
	if err := c.Authenticate(subsonicPass); err != nil {
		log.Fatalf("Failed to create Subsonic client: %s", err)
	}

	if *debugMode {
		log.Printf("Filters: album=%q artist=%q name=%q path=%q limit=%d", *filterAlbum, *filterArtist, *filterName, *filterPath, *limitTracks)
	}

	fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching subsonic data"))
	dstSongs, err := fetchSubsonicSongs(c, fetchBar)
	if err != nil {
		log.Fatalf("Failed fetching subsonic songs: %s", err)
	}

	log.Printf("Src track count %d, Dst track count %d\n", len(srcSongs), len(dstSongs))

	if *itunesRoot == "" && *subsonicRoot == "" {
		s := make([]i2s.SongInfo, 0, len(srcSongs))
		for _, si := range srcSongs {
			s = append(s, si)
		}
		d := make([]i2s.SongInfo, 0, len(dstSongs))
		for _, si := range dstSongs {
			d = append(d, si)
		}
		*itunesRoot, *subsonicRoot = i2s.LibraryPrefix(s, d)
	}
	fmt.Printf("Music library root: src='%s' dst='%s'\n", *itunesRoot, *subsonicRoot)

	byPath := make(map[string]*songPair)
	byTrackID := make(map[int]*songPair)
	filterActive := *filterAlbum != "" || *filterArtist != "" || *filterName != "" || *filterPath != "" || *limitTracks > 0
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

	fmt.Printf("== Copy %d Ratings To Subsonic ==\n", mismatchCount)
	if *dryRun {
		fmt.Printf("Set --dry_run=false to modify %s", *subsonicUrl)
	} else {
		fmt.Printf("== Copy %d Ratings To Subsonic ==\n", mismatchCount)
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
		fmt.Printf("== Sync %d Play Counts To Subsonic ==\n", playUpdates)
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
