package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/delucks/go-subsonic"
	i2s "github.com/logank/itunes2subsonic"
	"github.com/logank/itunes2subsonic/internal/itunes"
	"github.com/logank/itunes2subsonic/internal/report"
	pb "github.com/schollz/progressbar/v3"
)

const (
	reasonRemoteNoLocalMapping   = "remote_track_no_local_mapping"
	reasonNotInNavidromeIndex    = "not_in_navidrome_index"
	reasonExcludedExtension      = "excluded_extension"
	reasonInvalidLocation        = "invalid_location"
	reasonStaleMissingOnDisk     = "stale_missing_on_disk"
	reasonAmbiguousMatchMultiple = "ambiguous_match_multiple_dst"
	reasonFilteredOut            = "filtered_out"
)

type filterOptions struct {
	album  string
	artist string
	name   string
	path   string
	limit  int
}

type appleTrackInfo struct {
	track       itunes.Track
	location    locationParseResult
	filterPath  string
	filteredOut bool
	trackType   string
	loved       bool
	rated       bool
}

type navidromeSong struct {
	ID        string
	Path      string
	Title     string
	Artist    string
	Album     string
	Rating    int
	MatchKey  string
	CleanPath string
}

type navidromeStarredSong struct {
	ID     string
	Path   string
	Title  string
	Artist string
	Album  string
}

func isLovedTrack(track itunes.Track) bool {
	if track.Favorited != nil && *track.Favorited {
		return true
	}
	if track.Loved != nil && *track.Loved {
		return true
	}
	if track.Favorited != nil {
		return *track.Favorited
	}
	if track.Loved != nil {
		return *track.Loved
	}
	return false
}

func buildAppleTrackReport(info appleTrackInfo, matchKey string) report.AppleTrack {
	return report.AppleTrack{
		TrackID:   info.track.TrackId,
		Name:      info.track.Name,
		Artist:    info.track.Artist,
		Album:     info.track.Album,
		TrackType: info.trackType,
		Rating:    info.track.Rating,
		Loved:     info.loved,
		PathRaw:   info.location.raw,
		PathClean: info.location.parsed,
		MatchKey:  matchKey,
	}
}

func buildLibraryStats(itunesXML string, filters filterOptions, applyFilters bool) (report.LibraryStats, error) {
	if itunesXML == "" {
		return report.LibraryStats{}, fmt.Errorf("--itunes_xml is required")
	}
	file, err := os.Open(itunesXML)
	if err != nil {
		return report.LibraryStats{}, err
	}
	defer file.Close()

	library, err := itunes.LoadLibrary(file)
	if err != nil {
		return report.LibraryStats{}, err
	}

	stats := report.LibraryStats{}
	matchedCount := 0
	for _, track := range library.Tracks {
		location := parseLocation(track.Location)
		filterPathValue := location.parsed
		if filterPathValue == "" {
			if location.decoded != "" {
				filterPathValue = location.decoded
			} else {
				filterPathValue = location.raw
			}
		}
		if applyFilters {
			if !matchesFilter(track.Album, filters.album) || !matchesFilter(track.Artist, filters.artist) || !matchesFilter(track.Name, filters.name) || !matchesFilter(filterPathValue, filters.path) {
				continue
			}
			if filters.limit > 0 {
				if matchedCount >= filters.limit {
					break
				}
				matchedCount++
			}
		}

		trackType := "Local"
		if isRemoteTrack(track) {
			trackType = "Remote"
		}
		loved := isLovedTrack(track)
		rated := track.Rating > 0

		if loved {
			stats.Loved.Total++
			if trackType == "Remote" {
				stats.Loved.Remote++
			} else {
				stats.Loved.Local++
			}
		}
		if rated {
			stats.Rated.Total++
			if trackType == "Remote" {
				stats.Rated.Remote++
			} else {
				stats.Rated.Local++
			}
		}
		if loved && rated {
			stats.LovedAndRated.Total++
			if trackType == "Remote" {
				stats.LovedAndRated.Remote++
			} else {
				stats.LovedAndRated.Local++
			}
		}
	}
	return stats, nil
}

func loadAppleTracks(itunesXML string, filters filterOptions, allowlist map[string]struct{}, verifySrcFiles bool, includeFilteredInStats bool) ([]appleTrackInfo, []itunesInfo, report.LibraryStats, error) {
	if itunesXML == "" {
		return nil, nil, report.LibraryStats{}, fmt.Errorf("--itunes_xml is required")
	}
	file, err := os.Open(itunesXML)
	if err != nil {
		return nil, nil, report.LibraryStats{}, err
	}
	defer file.Close()

	library, err := itunes.LoadLibrary(file)
	if err != nil {
		return nil, nil, report.LibraryStats{}, err
	}

	stats := report.LibraryStats{}
	tracks := make([]appleTrackInfo, 0, len(library.Tracks))
	eligible := make([]itunesInfo, 0, len(library.Tracks))
	matchedCount := 0

	for _, track := range library.Tracks {
		location := parseLocation(track.Location)
		filterPathValue := location.parsed
		if filterPathValue == "" {
			if location.decoded != "" {
				filterPathValue = location.decoded
			} else {
				filterPathValue = location.raw
			}
		}

		filteredOut := false
		if !matchesFilter(track.Album, filters.album) || !matchesFilter(track.Artist, filters.artist) || !matchesFilter(track.Name, filters.name) || !matchesFilter(filterPathValue, filters.path) {
			filteredOut = true
		} else if filters.limit > 0 {
			if matchedCount >= filters.limit {
				filteredOut = true
			} else {
				matchedCount++
			}
		} else {
			matchedCount++
		}

		trackType := "Local"
		if isRemoteTrack(track) {
			trackType = "Remote"
		}
		loved := isLovedTrack(track)
		rated := track.Rating > 0

		if includeFilteredInStats || !filteredOut {
			if loved {
				stats.Loved.Total++
				if trackType == "Remote" {
					stats.Loved.Remote++
				} else {
					stats.Loved.Local++
				}
			}
			if rated {
				stats.Rated.Total++
				if trackType == "Remote" {
					stats.Rated.Remote++
				} else {
					stats.Rated.Local++
				}
			}
			if loved && rated {
				stats.LovedAndRated.Total++
				if trackType == "Remote" {
					stats.LovedAndRated.Remote++
				} else {
					stats.LovedAndRated.Local++
				}
			}
		}

		info := appleTrackInfo{
			track:       track,
			location:    location,
			filterPath:  filterPathValue,
			filteredOut: filteredOut,
			trackType:   trackType,
			loved:       loved,
			rated:       rated,
		}
		tracks = append(tracks, info)

		if filteredOut {
			continue
		}
		if isRemoteTrack(track) {
			continue
		}
		if !location.ok || isInvalidParsedPath(location.parsed) {
			continue
		}
		if _, allowed := isExtensionAllowed(location.parsed, allowlist); !allowed {
			continue
		}
		if verifySrcFiles {
			if _, err := os.Stat(location.parsed); err != nil {
				continue
			}
		}

		eligible = append(eligible, itunesInfo{
			id:        track.TrackId,
			path:      location.parsed,
			name:      track.Name,
			artist:    track.Artist,
			album:     track.Album,
			rating:    track.Rating,
			playDate:  track.PlayDateUTC,
			dateAdded: track.DateAdded,
			playCount: track.PlayCount,
			loved:     track.Loved != nil && *track.Loved,
			favorited: track.Favorited != nil && *track.Favorited,
			hasLoved:  track.Loved != nil,
			hasFav:    track.Favorited != nil,
		})
	}

	return tracks, eligible, stats, nil
}

func fetchStarredSongs(c *subsonic.Client) ([]navidromeStarredSong, error) {
	resp, err := c.GetStarred2(nil)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	songs := make([]navidromeStarredSong, 0, len(resp.Song))
	for _, song := range resp.Song {
		songs = append(songs, navidromeStarredSong{
			ID:     song.ID,
			Path:   song.Path,
			Title:  song.Title,
			Artist: song.Artist,
			Album:  song.Album,
		})
	}
	return songs, nil
}

func buildNavidromeSongsFromDump(entries []navidromeDumpEntry, root string, mode matchModeValue, allowlist map[string]struct{}) []navidromeSong {
	songs := make([]navidromeSong, 0, len(entries))
	for _, entry := range entries {
		cleaned := firstNonEmpty(entry.CleanPath, entry.DecodedPath, entry.Path)
		if cleaned == "" {
			continue
		}
		if _, allowed := isExtensionAllowed(cleaned, allowlist); !allowed {
			continue
		}
		matchKey := entry.MatchPath
		if matchKey == "" {
			matchKey = normalizeMatchPathWithMode(entry.Path, root, mode)
		}
		songs = append(songs, navidromeSong{
			ID:        entry.ID,
			Path:      entry.Path,
			MatchKey:  matchKey,
			CleanPath: cleaned,
		})
	}
	return songs
}

func buildNavidromeSongsFromSearch(songs []subsonicInfo, root string, mode matchModeValue, allowlist map[string]struct{}) []navidromeSong {
	result := make([]navidromeSong, 0, len(songs))
	for _, song := range songs {
		decoded := safePathUnescape(song.Path())
		cleaned := filepath.Clean(filepath.FromSlash(decoded))
		if _, allowed := isExtensionAllowed(cleaned, allowlist); !allowed {
			continue
		}
		matchKey := normalizeMatchPathWithMode(song.Path(), root, mode)
		result = append(result, navidromeSong{
			ID:        song.Id(),
			Path:      song.Path(),
			Title:     song.title,
			Artist:    song.artist,
			Album:     song.album,
			Rating:    song.rating,
			MatchKey:  matchKey,
			CleanPath: cleaned,
		})
	}
	return result
}

func buildNavidromeIndex(songs []navidromeSong) (map[string][]navidromeSong, map[string]navidromeSong) {
	byMatch := make(map[string][]navidromeSong)
	byID := make(map[string]navidromeSong)
	for _, song := range songs {
		if song.MatchKey != "" {
			byMatch[song.MatchKey] = append(byMatch[song.MatchKey], song)
		}
		if song.ID != "" {
			byID[song.ID] = song
		}
	}
	return byMatch, byID
}

func ensureNavidromeMetadata(c *subsonic.Client, song navidromeSong) (navidromeSong, error) {
	if song.Title != "" || song.Artist != "" || song.Album != "" {
		return song, nil
	}
	resp, err := c.GetSong(song.ID)
	if err != nil {
		return song, err
	}
	song.Title = resp.Title
	song.Artist = resp.Artist
	song.Album = resp.Album
	song.Rating = resp.UserRating
	song.Path = resp.Path
	return song, nil
}

func buildNotAppliedReason(info appleTrackInfo, allowlist map[string]struct{}, verifySrcFiles bool) (string, string) {
	if info.filteredOut {
		return reasonFilteredOut, ""
	}
	if isRemoteTrack(info.track) && (!info.location.ok || isInvalidParsedPath(info.location.parsed)) {
		return reasonRemoteNoLocalMapping, ""
	}
	if !info.location.ok || isInvalidParsedPath(info.location.parsed) {
		return reasonInvalidLocation, ""
	}
	if _, allowed := isExtensionAllowed(info.location.parsed, allowlist); !allowed {
		return reasonExcludedExtension, ""
	}
	if verifySrcFiles {
		if _, err := os.Stat(info.location.parsed); err != nil {
			return reasonStaleMissingOnDisk, ""
		}
	}
	matchKey := normalizeMatchPathWithMode(info.location.parsed, *itunesRoot, matchModeValue(*matchMode))
	if matchKey == "" {
		if isRemoteTrack(info.track) {
			return reasonRemoteNoLocalMapping, ""
		}
		return reasonInvalidLocation, ""
	}
	return "", matchKey
}

func runReportLibraryStats(itunesXML string, filters filterOptions, outJSON string, outTSV string) error {
	stats, err := buildLibraryStats(itunesXML, filters, true)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdoutWriter, "Apple Loved: total=%d local=%d remote=%d\n", stats.Loved.Total, stats.Loved.Local, stats.Loved.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated: total=%d local=%d remote=%d\n", stats.Rated.Total, stats.Rated.Local, stats.Rated.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved & Rated: total=%d local=%d remote=%d\n", stats.LovedAndRated.Total, stats.LovedAndRated.Local, stats.LovedAndRated.Remote)

	if err := report.WriteJSON(outJSON, stats); err != nil {
		return err
	}
	if outTSV != "" {
		rows := [][]string{
			{"Loved", strconv.Itoa(stats.Loved.Total), strconv.Itoa(stats.Loved.Local), strconv.Itoa(stats.Loved.Remote)},
			{"Rated", strconv.Itoa(stats.Rated.Total), strconv.Itoa(stats.Rated.Local), strconv.Itoa(stats.Rated.Remote)},
			{"LovedAndRated", strconv.Itoa(stats.LovedAndRated.Total), strconv.Itoa(stats.LovedAndRated.Local), strconv.Itoa(stats.LovedAndRated.Remote)},
		}
		if err := report.WriteTSV(outTSV, []string{"category", "total", "local", "remote"}, rows); err != nil {
			return err
		}
	}
	return nil
}

func runReportSyncPlan(c *subsonic.Client, itunesXML string, planPath string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool) error {
	appleTracks, eligibleSrc, stats, err := loadAppleTracks(itunesXML, filters, allowlist, *verifySrcFiles, true)
	if err != nil {
		return err
	}

	starredSongs, err := fetchStarredSongs(c)
	if err != nil {
		return err
	}
	starredByID := make(map[string]navidromeStarredSong, len(starredSongs))
	for _, song := range starredSongs {
		starredByID[song.ID] = song
	}

	var (
		navidromeSongs   []navidromeSong
		navidromeByMatch map[string][]navidromeSong
	)
	if reportOnly && filterActive {
		if *dumpFile == "" {
			return fmt.Errorf("--report_only requires --navidrome_dump when filters are active")
		}
		entries, err := loadNavidromeDump(*dumpFile)
		if err != nil {
			return err
		}
		navidromeSongs = buildNavidromeSongsFromDump(entries, *subsonicRoot, selectedMatchMode, allowlist)
		navidromeByMatch, _ = buildNavidromeIndex(navidromeSongs)
	} else {
		fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
		dstSongs, err := fetchSubsonicSongs(c, fetchBar)
		if err != nil {
			return err
		}
		if *itunesRoot == "" && *subsonicRoot == "" && !filterActive {
			srcInfo := make([]i2s.SongInfo, 0, len(eligibleSrc))
			for _, si := range eligibleSrc {
				srcInfo = append(srcInfo, si)
			}
			dstInfo := make([]i2s.SongInfo, 0, len(dstSongs))
			for _, si := range dstSongs {
				dstInfo = append(dstInfo, si)
			}
			*itunesRoot, *subsonicRoot = i2s.LibraryPrefix(srcInfo, dstInfo)
		}
		navidromeSongs = buildNavidromeSongsFromSearch(dstSongs, *subsonicRoot, selectedMatchMode, allowlist)
		navidromeByMatch, _ = buildNavidromeIndex(navidromeSongs)
	}

	plan := report.SyncPlan{
		Counts: report.SyncPlanCounts{
			AppleLoved: stats.Loved,
			AppleRated: stats.Rated,
			LovedNotApplied: report.PlanReasonCounts{
				ByReason: make(map[string]int),
			},
			RatedNotApplied: report.PlanReasonCounts{
				ByReason: make(map[string]int),
			},
		},
		Loved:   report.SyncPlanLoved{},
		Ratings: report.SyncPlanRatings{},
		Unstar:  report.SyncPlanUnstar{},
	}

	for _, info := range appleTracks {
		if !info.loved {
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, *verifySrcFiles)
		appleReport := buildAppleTrackReport(info, matchKey)
		if reason != "" {
			plan.Loved.WontStar = append(plan.Loved.WontStar, report.LovedPlanEntry{
				Apple:            appleReport,
				NotAppliedReason: reason,
			})
			plan.Counts.LovedNotApplied.Total++
			plan.Counts.LovedNotApplied.ByReason[reason]++
			continue
		}
		matches := navidromeByMatch[matchKey]
		if len(matches) == 0 {
			reason = reasonNotInNavidromeIndex
		} else if len(matches) > 1 {
			reason = reasonAmbiguousMatchMultiple
		}
		if reason != "" {
			plan.Loved.WontStar = append(plan.Loved.WontStar, report.LovedPlanEntry{
				Apple:            appleReport,
				NotAppliedReason: reason,
			})
			plan.Counts.LovedNotApplied.Total++
			plan.Counts.LovedNotApplied.ByReason[reason]++
			continue
		}

		match := matches[0]
		if reportOnly && filterActive {
			updated, err := ensureNavidromeMetadata(c, match)
			if err != nil {
				return err
			}
			match = updated
		}
		if _, ok := starredByID[match.ID]; ok {
			continue
		}
		plan.Loved.WillStar = append(plan.Loved.WillStar, report.LovedPlanEntry{
			Apple: appleReport,
			Navidrome: &report.NavidromeTrack{
				SongID: match.ID,
				Path:   match.Path,
				Title:  match.Title,
				Artist: match.Artist,
				Album:  match.Album,
			},
		})
		plan.Counts.PlannedStar.Total++
		if info.trackType == "Remote" {
			plan.Counts.PlannedStar.Remote++
		} else {
			plan.Counts.PlannedStar.Local++
		}
	}

	for _, info := range appleTracks {
		if !info.rated {
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, *verifySrcFiles)
		appleReport := buildAppleTrackReport(info, matchKey)
		if reason != "" {
			plan.Ratings.WontSet = append(plan.Ratings.WontSet, report.RatingPlanEntry{
				Apple:            appleReport,
				DesiredRating:    appleReport.Rating / 20,
				NotAppliedReason: reason,
			})
			plan.Counts.RatedNotApplied.Total++
			plan.Counts.RatedNotApplied.ByReason[reason]++
			continue
		}
		matches := navidromeByMatch[matchKey]
		if len(matches) == 0 {
			reason = reasonNotInNavidromeIndex
		} else if len(matches) > 1 {
			reason = reasonAmbiguousMatchMultiple
		}
		if reason != "" {
			plan.Ratings.WontSet = append(plan.Ratings.WontSet, report.RatingPlanEntry{
				Apple:            appleReport,
				DesiredRating:    appleReport.Rating / 20,
				NotAppliedReason: reason,
			})
			plan.Counts.RatedNotApplied.Total++
			plan.Counts.RatedNotApplied.ByReason[reason]++
			continue
		}
		match := matches[0]
		if reportOnly && filterActive {
			updated, err := ensureNavidromeMetadata(c, match)
			if err != nil {
				return err
			}
			match = updated
		}
		desiredRating := appleReport.Rating / 20
		if match.Rating == desiredRating {
			continue
		}
		plan.Ratings.WillSet = append(plan.Ratings.WillSet, report.RatingPlanEntry{
			Apple:         appleReport,
			DesiredRating: desiredRating,
			Navidrome: &report.NavidromeTrack{
				SongID: match.ID,
				Path:   match.Path,
				Title:  match.Title,
				Artist: match.Artist,
				Album:  match.Album,
				Rating: match.Rating,
			},
		})
		plan.Counts.PlannedRatings++
	}

	appleByMatch := make(map[string]appleTrackInfo)
	for _, info := range appleTracks {
		if info.location.parsed == "" {
			continue
		}
		matchKey := normalizeMatchPathWithMode(info.location.parsed, *itunesRoot, selectedMatchMode)
		if matchKey == "" {
			continue
		}
		if _, exists := appleByMatch[matchKey]; !exists {
			appleByMatch[matchKey] = info
		}
	}

	for _, song := range starredSongs {
		matchKey := normalizeMatchPathWithMode(song.Path, *subsonicRoot, selectedMatchMode)
		var (
			appleMatch *report.AppleTrack
			reason     string
		)
		if matchKey != "" {
			if info, ok := appleByMatch[matchKey]; ok {
				apple := buildAppleTrackReport(info, matchKey)
				appleMatch = &apple
				if info.loved {
					continue
				}
				reason = "starred_in_navidrome_but_not_loved_in_apple"
			}
		}
		if reason == "" && appleMatch == nil {
			reason = "starred_no_apple_match"
		}
		if reason == "" {
			continue
		}
		plan.Unstar.WillUnstar = append(plan.Unstar.WillUnstar, report.UnstarPlanEntry{
			Navidrome: report.NavidromeTrack{
				SongID: song.ID,
				Path:   song.Path,
				Title:  song.Title,
				Artist: song.Artist,
				Album:  song.Album,
			},
			Apple:  appleMatch,
			Reason: reason,
		})
	}
	plan.Counts.PlannedUnstar = len(plan.Unstar.WillUnstar)

	sortLovedPlanEntries(plan.Loved.WontStar)
	sortRatingPlanEntries(plan.Ratings.WontSet)
	sortUnstarPlanEntries(plan.Unstar.WillUnstar)

	fmt.Fprintf(stdoutWriter, "Apple Loved: total=%d local=%d remote=%d\n", plan.Counts.AppleLoved.Total, plan.Counts.AppleLoved.Local, plan.Counts.AppleLoved.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated: total=%d local=%d remote=%d\n", plan.Counts.AppleRated.Total, plan.Counts.AppleRated.Local, plan.Counts.AppleRated.Remote)
	fmt.Fprintf(stdoutWriter, "Planned Star: %d (local=%d remote=%d)\n", plan.Counts.PlannedStar.Total, plan.Counts.PlannedStar.Local, plan.Counts.PlannedStar.Remote)
	fmt.Fprintf(stdoutWriter, "Planned Unstar: %d\n", plan.Counts.PlannedUnstar)
	for _, entry := range plan.Unstar.WillUnstar {
		fmt.Fprintf(stdoutWriter, "  - %s - %s (%s) [%s]\n", entry.Navidrome.Artist, entry.Navidrome.Title, entry.Navidrome.Album, entry.Navidrome.SongID)
	}
	fmt.Fprintf(stdoutWriter, "Planned Rating Updates: %d\n", plan.Counts.PlannedRatings)
	fmt.Fprintf(stdoutWriter, "Loved not applied: %d\n", plan.Counts.LovedNotApplied.Total)
	printReasonCounts(plan.Counts.LovedNotApplied.ByReason)
	fmt.Fprintf(stdoutWriter, "Rated not applied: %d\n", plan.Counts.RatedNotApplied.Total)
	printReasonCounts(plan.Counts.RatedNotApplied.ByReason)

	if err := report.WriteJSON(planPath, plan); err != nil {
		return err
	}

	planDir := filepath.Dir(planPath)
	unstarPath := filepath.Join(planDir, "plan_unstar.tsv")
	lovedNotAppliedPath := filepath.Join(planDir, "plan_loved_not_applied.tsv")
	ratedNotAppliedPath := filepath.Join(planDir, "plan_rated_not_applied.tsv")

	if err := report.WriteTSV(unstarPath, []string{
		"navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path",
		"apple_track_id", "apple_loved", "apple_path", "reason",
	}, buildUnstarRows(plan.Unstar.WillUnstar)); err != nil {
		return err
	}
	if err := report.WriteTSV(lovedNotAppliedPath, []string{
		"reason", "apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_track_type",
		"apple_rating", "apple_loved", "apple_path_raw", "apple_path_clean", "apple_match_key",
		"navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path",
	}, buildLovedNotAppliedRows(plan.Loved.WontStar)); err != nil {
		return err
	}
	if err := report.WriteTSV(ratedNotAppliedPath, []string{
		"reason", "apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_track_type",
		"apple_rating", "apple_loved", "apple_path_raw", "apple_path_clean", "apple_match_key",
		"navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path",
	}, buildRatedNotAppliedRows(plan.Ratings.WontSet)); err != nil {
		return err
	}

	return nil
}

func printReasonCounts(counts map[string]int) {
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdoutWriter, "  - %s: %d\n", key, counts[key])
	}
}

func sortLovedPlanEntries(entries []report.LovedPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		if a.NotAppliedReason != b.NotAppliedReason {
			return a.NotAppliedReason < b.NotAppliedReason
		}
		if a.Apple.Artist != b.Apple.Artist {
			return a.Apple.Artist < b.Apple.Artist
		}
		if a.Apple.Album != b.Apple.Album {
			return a.Apple.Album < b.Apple.Album
		}
		if a.Apple.Name != b.Apple.Name {
			return a.Apple.Name < b.Apple.Name
		}
		return a.Apple.TrackID < b.Apple.TrackID
	})
}

func sortRatingPlanEntries(entries []report.RatingPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		if a.NotAppliedReason != b.NotAppliedReason {
			return a.NotAppliedReason < b.NotAppliedReason
		}
		if a.Apple.Artist != b.Apple.Artist {
			return a.Apple.Artist < b.Apple.Artist
		}
		if a.Apple.Album != b.Apple.Album {
			return a.Apple.Album < b.Apple.Album
		}
		if a.Apple.Name != b.Apple.Name {
			return a.Apple.Name < b.Apple.Name
		}
		return a.Apple.TrackID < b.Apple.TrackID
	})
}

func sortUnstarPlanEntries(entries []report.UnstarPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		if a.Reason != b.Reason {
			return a.Reason < b.Reason
		}
		if a.Navidrome.Artist != b.Navidrome.Artist {
			return a.Navidrome.Artist < b.Navidrome.Artist
		}
		if a.Navidrome.Album != b.Navidrome.Album {
			return a.Navidrome.Album < b.Navidrome.Album
		}
		if a.Navidrome.Title != b.Navidrome.Title {
			return a.Navidrome.Title < b.Navidrome.Title
		}
		return a.Navidrome.SongID < b.Navidrome.SongID
	})
}

func buildUnstarRows(entries []report.UnstarPlanEntry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		appleID := ""
		appleLoved := ""
		applePath := ""
		if entry.Apple != nil {
			appleID = strconv.Itoa(entry.Apple.TrackID)
			appleLoved = strconv.FormatBool(entry.Apple.Loved)
			applePath = entry.Apple.PathClean
		}
		rows = append(rows, []string{
			entry.Navidrome.SongID,
			entry.Navidrome.Title,
			entry.Navidrome.Artist,
			entry.Navidrome.Album,
			entry.Navidrome.Path,
			appleID,
			appleLoved,
			applePath,
			entry.Reason,
		})
	}
	return rows
}

func buildLovedNotAppliedRows(entries []report.LovedPlanEntry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		nav := entry.Navidrome
		rows = append(rows, []string{
			entry.NotAppliedReason,
			strconv.Itoa(entry.Apple.TrackID),
			entry.Apple.Name,
			entry.Apple.Artist,
			entry.Apple.Album,
			entry.Apple.TrackType,
			strconv.Itoa(entry.Apple.Rating),
			strconv.FormatBool(entry.Apple.Loved),
			entry.Apple.PathRaw,
			entry.Apple.PathClean,
			entry.Apple.MatchKey,
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.SongID }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Title }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Artist }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Album }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Path }),
		})
	}
	return rows
}

func buildRatedNotAppliedRows(entries []report.RatingPlanEntry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		nav := entry.Navidrome
		rows = append(rows, []string{
			entry.NotAppliedReason,
			strconv.Itoa(entry.Apple.TrackID),
			entry.Apple.Name,
			entry.Apple.Artist,
			entry.Apple.Album,
			entry.Apple.TrackType,
			strconv.Itoa(entry.Apple.Rating),
			strconv.FormatBool(entry.Apple.Loved),
			entry.Apple.PathRaw,
			entry.Apple.PathClean,
			entry.Apple.MatchKey,
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.SongID }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Title }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Artist }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Album }),
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Path }),
		})
	}
	return rows
}

func navidromeValue(value *report.NavidromeTrack, getter func(*report.NavidromeTrack) string) string {
	if value == nil {
		return ""
	}
	return getter(value)
}
