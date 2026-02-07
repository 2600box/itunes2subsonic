package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	reasonAlreadyStarred         = "already_starred"
	reasonAlreadyUnstarred       = "already_unstarred"
	reasonAlreadyRated           = "already_rated"
	reasonAlreadyUnrated         = "already_unrated"
	reasonCopyUnratedDisabled    = "copy_unrated_disabled"
	reasonPlayCountNoData        = "no_playcount_data"
	reasonPlayCountUpToDate      = "playcount_up_to_date"
	reasonPlayCountDisabled      = "playcount_sync_disabled"
	reasonStarredNotLoved        = "starred_in_navidrome_but_not_loved_in_apple"
	reasonStarredNoAppleMatch    = "starred_no_apple_match"
	reasonPlaylistNoTracks       = "playlist_empty"
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
	PlayCount int64
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

type playlistPlanContext struct {
	navidromeByID map[string]navidromeSong
	appleByID     map[int]appleTrackInfo
	appleByMatch  map[string]appleTrackInfo
	navByMatch    map[string][]navidromeSong
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

func buildNavidromeTrackReport(song navidromeSong) report.NavidromeTrack {
	return report.NavidromeTrack{
		SongID: song.ID,
		Path:   song.Path,
		Title:  song.Title,
		Artist: song.Artist,
		Album:  song.Album,
		Rating: song.Rating,
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
		stats.Tracks.Total++
		if trackType == "Remote" {
			stats.Tracks.Remote++
		} else {
			stats.Tracks.Local++
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
		lovedOnly := loved && !rated
		ratedOnly := rated && !loved

		if loved && rated {
			stats.LovedAndRated.Total++
			if trackType == "Remote" {
				stats.LovedAndRated.Remote++
			} else {
				stats.LovedAndRated.Local++
			}
		}
		if lovedOnly {
			stats.LovedOnly.Total++
			if trackType == "Remote" {
				stats.LovedOnly.Remote++
			} else {
				stats.LovedOnly.Local++
			}
		}
		if ratedOnly {
			stats.RatedOnly.Total++
			if trackType == "Remote" {
				stats.RatedOnly.Remote++
			} else {
				stats.RatedOnly.Local++
			}
		}
	}
	return stats, nil
}

func loadAppleTracks(itunesXML string, filters filterOptions, allowlist map[string]struct{}, verifySrcFiles bool, includeFilteredInStats bool) ([]appleTrackInfo, []itunesInfo, []playlistRef, report.LibraryStats, error) {
	if itunesXML == "" {
		return nil, nil, nil, report.LibraryStats{}, fmt.Errorf("--itunes_xml is required")
	}
	file, err := os.Open(itunesXML)
	if err != nil {
		return nil, nil, nil, report.LibraryStats{}, err
	}
	defer file.Close()

	library, err := itunes.LoadLibrary(file)
	if err != nil {
		return nil, nil, nil, report.LibraryStats{}, err
	}

	stats := report.LibraryStats{}
	tracks := make([]appleTrackInfo, 0, len(library.Tracks))
	eligible := make([]itunesInfo, 0, len(library.Tracks))
	matchedCount := 0
	playlists := make([]playlistRef, 0, len(library.Playlists))
	for _, playlist := range library.Playlists {
		playlists = append(playlists, playlistRef{Name: playlist.Name, Master: playlist.Master, Items: playlist.PlaylistItems})
	}

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
		if includeFilteredInStats || !filteredOut {
			stats.Tracks.Total++
			if trackType == "Remote" {
				stats.Tracks.Remote++
			} else {
				stats.Tracks.Local++
			}
		}
		loved := isLovedTrack(track)
		rated := track.Rating > 0

		lovedOnly := loved && !rated
		ratedOnly := rated && !loved

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
			if lovedOnly {
				stats.LovedOnly.Total++
				if trackType == "Remote" {
					stats.LovedOnly.Remote++
				} else {
					stats.LovedOnly.Local++
				}
			}
			if ratedOnly {
				stats.RatedOnly.Total++
				if trackType == "Remote" {
					stats.RatedOnly.Remote++
				} else {
					stats.RatedOnly.Local++
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

	return tracks, eligible, playlists, stats, nil
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
			Title:     entry.Title,
			Artist:    entry.Artist,
			Album:     entry.Album,
			Rating:    entry.Rating,
			PlayCount: entry.PlayCount,
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
			PlayCount: song.playCount,
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

	fmt.Fprintf(stdoutWriter, "Apple Tracks: total=%d local=%d remote=%d\n", stats.Tracks.Total, stats.Tracks.Local, stats.Tracks.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved: total=%d local=%d remote=%d\n", stats.Loved.Total, stats.Loved.Local, stats.Loved.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated: total=%d local=%d remote=%d\n", stats.Rated.Total, stats.Rated.Local, stats.Rated.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved & Rated: total=%d local=%d remote=%d\n", stats.LovedAndRated.Total, stats.LovedAndRated.Local, stats.LovedAndRated.Remote)

	if err := report.WriteJSON(outJSON, stats); err != nil {
		return err
	}
	if outTSV != "" {
		rows := [][]string{
			{"Tracks", strconv.Itoa(stats.Tracks.Total), strconv.Itoa(stats.Tracks.Local), strconv.Itoa(stats.Tracks.Remote)},
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

func buildSyncPlan(c *subsonic.Client, itunesXML string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool) (report.SyncPlan, report.LibraryStats, []navidromeSong, []navidromeStarredSong, []subsonicInfo, []appleTrackInfo, error) {
	if reportOnly && *dumpFile == "" {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, nil, nil, fmt.Errorf("--report_only requires --navidrome_dump to build a sync plan")
	}
	appleTracks, eligibleSrc, playlistRefs, stats, err := loadAppleTracks(itunesXML, filters, allowlist, *verifySrcFiles, true)
	if err != nil {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, nil, nil, err
	}

	starredSongs, err := fetchStarredSongs(c)
	if err != nil {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, nil, nil, err
	}
	starredByID := make(map[string]navidromeStarredSong, len(starredSongs))
	for _, song := range starredSongs {
		starredByID[song.ID] = song
	}

	var navidromeSongs []navidromeSong
	var dstSongs []subsonicInfo
	if reportOnly {
		entries, err := loadNavidromeDump(*dumpFile)
		if err != nil {
			return report.SyncPlan{}, report.LibraryStats{}, nil, nil, nil, nil, err
		}
		navidromeSongs = buildNavidromeSongsFromDump(entries, *subsonicRoot, selectedMatchMode, allowlist)
	} else {
		fetchBar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
		songs, err := fetchSubsonicSongs(c, fetchBar)
		if err != nil {
			return report.SyncPlan{}, report.LibraryStats{}, nil, nil, nil, nil, err
		}
		dstSongs = songs
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
	}

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	plan := report.SyncPlan{
		SchemaVersion: 1,
		GeneratedAt:   generatedAt,
		NavidromeSummary: report.NavidromeSummary{
			TracksTotal:  len(navidromeSongs),
			StarredTotal: len(starredSongs),
			RatedTotal:   countRatedNavidrome(navidromeSongs),
		},
		Counts: report.SyncPlanCounts{
			AppleTracks:        stats.Tracks,
			AppleLoved:         stats.Loved,
			AppleRated:         stats.Rated,
			AppleLovedAndRated: stats.LovedAndRated,
			LovedNotApplied: report.PlanReasonCounts{
				ByReason: make(map[string]int),
			},
			RatedNotApplied: report.PlanReasonCounts{
				ByReason: make(map[string]int),
			},
		},
		Loved:     report.SyncPlanLoved{},
		Ratings:   report.SyncPlanRatings{},
		Unstar:    report.SyncPlanUnstar{},
		PlayCount: report.SyncPlanPlayCounts{},
		Playlists: report.SyncPlanPlaylists{},
	}

	navidromeByMatch, navidromeByID := buildNavidromeIndex(navidromeSongs)
	appleByMatch := make(map[string]appleTrackInfo)
	appleByID := make(map[int]appleTrackInfo)
	for _, info := range appleTracks {
		if info.track.TrackId != 0 {
			appleByID[info.track.TrackId] = info
		}
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

	for _, info := range appleTracks {
		if !info.loved {
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, *verifySrcFiles)
		appleReport := buildAppleTrackReport(info, matchKey)
		entry := report.LovedPlanEntry{
			Operation: "star",
			Apple:     appleReport,
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reason
			plan.Loved.WontStar = append(plan.Loved.WontStar, entry)
			incrementLovedNotApplied(&plan, info.trackType, reason)
			continue
		}
		matches := navidromeByMatch[matchKey]
		if len(matches) == 0 {
			reason = reasonNotInNavidromeIndex
		} else if len(matches) > 1 {
			reason = reasonAmbiguousMatchMultiple
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reason
			plan.Loved.WontStar = append(plan.Loved.WontStar, entry)
			incrementLovedNotApplied(&plan, info.trackType, reason)
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
		entry.Navidrome = ptrNavidromeTrack(buildNavidromeTrackReport(match))
		if _, ok := starredByID[match.ID]; ok {
			entry.Action = "noop"
			entry.Reason = reasonAlreadyStarred
			plan.Loved.Noop = append(plan.Loved.Noop, entry)
			continue
		}
		entry.Action = "star"
		plan.Loved.WillStar = append(plan.Loved.WillStar, entry)
		plan.Counts.PlannedStar.Total++
		if info.trackType == "Remote" {
			plan.Counts.PlannedStar.Remote++
		} else {
			plan.Counts.PlannedStar.Local++
		}
	}

	for _, info := range appleTracks {
		if !info.rated && !*copyUnrated {
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, *verifySrcFiles)
		appleReport := buildAppleTrackReport(info, matchKey)
		entry := report.RatingPlanEntry{
			Operation:     "rate",
			Apple:         appleReport,
			DesiredRating: appleReport.Rating / 20,
		}
		if !info.rated {
			entry.DesiredRating = 0
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reason
			plan.Ratings.WontSet = append(plan.Ratings.WontSet, entry)
			if info.rated {
				plan.Counts.RatedNotApplied.Total++
				plan.Counts.RatedNotApplied.ByReason[reason]++
			}
			continue
		}
		matches := navidromeByMatch[matchKey]
		if len(matches) == 0 {
			reason = reasonNotInNavidromeIndex
		} else if len(matches) > 1 {
			reason = reasonAmbiguousMatchMultiple
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reason
			plan.Ratings.WontSet = append(plan.Ratings.WontSet, entry)
			if info.rated {
				plan.Counts.RatedNotApplied.Total++
				plan.Counts.RatedNotApplied.ByReason[reason]++
			}
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
		entry.Navidrome = ptrNavidromeTrack(buildNavidromeTrackReport(match))
		if !info.rated && !*copyUnrated {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reasonCopyUnratedDisabled
			plan.Ratings.WontSet = append(plan.Ratings.WontSet, entry)
			continue
		}
		desiredRating := entry.DesiredRating
		if match.Rating == desiredRating {
			entry.Action = "noop"
			if desiredRating == 0 {
				entry.Reason = reasonAlreadyUnrated
			} else {
				entry.Reason = reasonAlreadyRated
			}
			plan.Ratings.Noop = append(plan.Ratings.Noop, entry)
			plan.Counts.PlannedRatingsNoop++
			continue
		}
		if desiredRating == 0 {
			entry.Action = "unset"
			plan.Ratings.WillUnset = append(plan.Ratings.WillUnset, entry)
			plan.Counts.PlannedRatingsUnset++
			continue
		}
		entry.Action = "set"
		plan.Ratings.WillSet = append(plan.Ratings.WillSet, entry)
		plan.Counts.PlannedRatingsSet++
	}

	for _, info := range appleTracks {
		hasPlayData := info.track.PlayCount > 0 || !info.track.PlayDateUTC.IsZero()
		if !hasPlayData {
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, *verifySrcFiles)
		appleReport := buildAppleTrackReport(info, matchKey)
		entry := report.PlayCountPlanEntry{
			Operation:       "playcount",
			Apple:           appleReport,
			ApplePlayCount:  info.track.PlayCount,
			AppleLastPlayed: formatTime(info.track.PlayDateUTC),
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.Reason = reason
			plan.PlayCount.WontUpdate = append(plan.PlayCount.WontUpdate, entry)
			continue
		}
		matches := navidromeByMatch[matchKey]
		if len(matches) == 0 {
			reason = reasonNotInNavidromeIndex
		} else if len(matches) > 1 {
			reason = reasonAmbiguousMatchMultiple
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.Reason = reason
			plan.PlayCount.WontUpdate = append(plan.PlayCount.WontUpdate, entry)
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
		entry.Navidrome = ptrNavidromeTrack(buildNavidromeTrackReport(match))
		entry.NavidromePlayCount = match.PlayCount
		srcCount := int64(info.track.PlayCount)
		if !*updatePlay {
			entry.Action = "noop"
			entry.Reason = reasonPlayCountDisabled
			plan.PlayCount.Noop = append(plan.PlayCount.Noop, entry)
			plan.Counts.PlannedPlaycountNoop++
			continue
		}
		if srcCount == 0 && info.track.PlayDateUTC.IsZero() {
			entry.Action = "noop"
			entry.Reason = reasonPlayCountNoData
			plan.PlayCount.Noop = append(plan.PlayCount.Noop, entry)
			plan.Counts.PlannedPlaycountNoop++
			continue
		}
		if srcCount <= match.PlayCount && (info.track.PlayDateUTC.IsZero() || match.PlayCount > 0) {
			entry.Action = "noop"
			entry.Reason = reasonPlayCountUpToDate
			plan.PlayCount.Noop = append(plan.PlayCount.Noop, entry)
			plan.Counts.PlannedPlaycountNoop++
			continue
		}
		desired := srcCount - match.PlayCount
		if desired <= 0 {
			entry.Action = "noop"
			entry.Reason = reasonPlayCountUpToDate
			plan.PlayCount.Noop = append(plan.PlayCount.Noop, entry)
			plan.Counts.PlannedPlaycountNoop++
			continue
		}
		if *maxScrobbles > 0 && desired > int64(*maxScrobbles) {
			desired = int64(*maxScrobbles)
		}
		entry.Action = "update"
		entry.DesiredScrobbleCount = desired
		plan.PlayCount.WillUpdate = append(plan.PlayCount.WillUpdate, entry)
		plan.Counts.PlannedPlaycountUpdates++
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
				reason = reasonStarredNotLoved
			}
		}
		entry := report.UnstarPlanEntry{
			Operation: "star",
			Action:    "unstar",
			Navidrome: report.NavidromeTrack{
				SongID: song.ID,
				Path:   song.Path,
				Title:  song.Title,
				Artist: song.Artist,
				Album:  song.Album,
			},
			Apple: appleMatch,
		}
		if reason == "" && appleMatch == nil {
			reason = reasonStarredNoAppleMatch
		}
		if reason == "" {
			continue
		}
		entry.Reason = reason
		if reason == reasonStarredNoAppleMatch {
			entry.Action = "wont_apply"
			plan.Unstar.WontUnstar = append(plan.Unstar.WontUnstar, entry)
			continue
		}
		plan.Unstar.WillUnstar = append(plan.Unstar.WillUnstar, entry)
	}
	plan.Counts.PlannedUnstar = len(plan.Unstar.WillUnstar)

	existingPlaylists, err := c.GetPlaylists(nil)
	if err != nil {
		return err
	}
	playlistsByName := make(map[string]*subsonic.Playlist)
	for _, playlist := range existingPlaylists {
		playlistsByName[playlist.Name] = playlist
	}

	for _, playlist := range playlistRefs {
		if playlist.Master || playlist.Name == "" {
			continue
		}
		entry, err := buildPlaylistPlanEntry(playlist, playlistPlanContext{
			navidromeByID: navidromeByID,
			appleByID:     appleByID,
			appleByMatch:  appleByMatch,
			navByMatch:    navidromeByMatch,
		}, allowlist, *verifySrcFiles, c, playlistsByName)
		if err != nil {
			return err
		}
		plan.Playlists.Entries = append(plan.Playlists.Entries, entry)
		switch entry.Action {
		case "create":
			plan.Counts.PlannedPlaylistCreates++
		case "update":
			plan.Counts.PlannedPlaylistUpdates++
		case "noop":
			plan.Counts.PlannedPlaylistNoop++
		}
		plan.Counts.PlannedPlaylistTrackAdds += len(entry.AddTracks)
		plan.Counts.PlannedPlaylistRemoves += len(entry.RemoveTracks)
	}

	sortLovedPlanEntries(plan.Loved.WillStar)
	sortLovedPlanEntries(plan.Loved.Noop)
	sortLovedPlanEntries(plan.Loved.WontStar)
	sortRatingPlanEntries(plan.Ratings.WillSet)
	sortRatingPlanEntries(plan.Ratings.WillUnset)
	sortRatingPlanEntries(plan.Ratings.Noop)
	sortRatingPlanEntries(plan.Ratings.WontSet)
	sortPlayCountPlanEntries(plan.PlayCount.WillUpdate)
	sortPlayCountPlanEntries(plan.PlayCount.Noop)
	sortPlayCountPlanEntries(plan.PlayCount.WontUpdate)
	sortUnstarPlanEntries(plan.Unstar.WillUnstar)
	sortUnstarPlanEntries(plan.Unstar.WontUnstar)
	sortPlaylistPlanEntries(plan.Playlists.Entries)

	return plan, stats, navidromeSongs, starredSongs, dstSongs, appleTracks, nil
}

func runReportSyncPlan(c *subsonic.Client, itunesXML string, planPath string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool) error {
	if planPath == "" {
		return fmt.Errorf("--report_sync_plan requires a path")
	}
	plan, stats, _, _, _, _, err := buildSyncPlan(c, itunesXML, filters, allowlist, selectedMatchMode, filterActive, reportOnly)
	if err != nil {
		return err
	}
	printPlanSummary(stats, plan)
	printDryRunSummary(stats, plan)
	return writeSyncPlanArtifacts(planPath, plan, selectedMatchMode, *reportSyncPlanTSV)
}

func runReportSyncPlanWithData(c *subsonic.Client, itunesXML string, planPath string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool, planTSVBase string) (report.SyncPlan, report.LibraryStats, []navidromeSong, []appleTrackInfo, error) {
	if planPath == "" {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, fmt.Errorf("--report_sync_plan requires a path")
	}
	plan, stats, navidromeSongs, _, _, appleTracks, err := buildSyncPlan(c, itunesXML, filters, allowlist, selectedMatchMode, filterActive, reportOnly)
	if err != nil {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, err
	}
	printPlanSummary(stats, plan)
	printDryRunSummary(stats, plan)
	if err := writeSyncPlanArtifacts(planPath, plan, selectedMatchMode, planTSVBase); err != nil {
		return report.SyncPlan{}, report.LibraryStats{}, nil, nil, err
	}
	return plan, stats, navidromeSongs, appleTracks, nil
}

func printPlanSummary(stats report.LibraryStats, plan report.SyncPlan) {
	fmt.Fprintf(stdoutWriter, "Apple Tracks: total=%d local=%d remote=%d\n", plan.Counts.AppleTracks.Total, plan.Counts.AppleTracks.Local, plan.Counts.AppleTracks.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved: total=%d local=%d remote=%d\n", plan.Counts.AppleLoved.Total, plan.Counts.AppleLoved.Local, plan.Counts.AppleLoved.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved Only: total=%d local=%d remote=%d\n", stats.LovedOnly.Total, stats.LovedOnly.Local, stats.LovedOnly.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved & Rated: total=%d local=%d remote=%d\n", plan.Counts.AppleLovedAndRated.Total, plan.Counts.AppleLovedAndRated.Local, plan.Counts.AppleLovedAndRated.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated: total=%d local=%d remote=%d\n", plan.Counts.AppleRated.Total, plan.Counts.AppleRated.Local, plan.Counts.AppleRated.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated Only: total=%d local=%d remote=%d\n", stats.RatedOnly.Total, stats.RatedOnly.Local, stats.RatedOnly.Remote)
	fmt.Fprintf(stdoutWriter, "Navidrome Starred Baseline: total=%d\n", plan.NavidromeSummary.StarredTotal)
	fmt.Fprintf(stdoutWriter, "Planned Star: %d (local=%d remote=%d)\n", plan.Counts.PlannedStar.Total, plan.Counts.PlannedStar.Local, plan.Counts.PlannedStar.Remote)
	fmt.Fprintf(stdoutWriter, "Planned Unstar: %d\n", plan.Counts.PlannedUnstar)
	fmt.Fprintf(stdoutWriter, "Planned Ratings: set=%d unset=%d noop=%d\n", plan.Counts.PlannedRatingsSet, plan.Counts.PlannedRatingsUnset, plan.Counts.PlannedRatingsNoop)
	fmt.Fprintf(stdoutWriter, "Planned Play Count Updates: update=%d noop=%d\n", plan.Counts.PlannedPlaycountUpdates, plan.Counts.PlannedPlaycountNoop)
	fmt.Fprintf(stdoutWriter, "Planned Playlists: create=%d update=%d noop=%d (adds=%d removes=%d)\n", plan.Counts.PlannedPlaylistCreates, plan.Counts.PlannedPlaylistUpdates, plan.Counts.PlannedPlaylistNoop, plan.Counts.PlannedPlaylistTrackAdds, plan.Counts.PlannedPlaylistRemoves)
	fmt.Fprintf(stdoutWriter, "Loved not applied: %d\n", plan.Counts.LovedNotApplied.Total)
	printReasonCounts(plan.Counts.LovedNotApplied.ByReason)
	fmt.Fprintf(stdoutWriter, "Rated not applied: %d\n", plan.Counts.RatedNotApplied.Total)
	printReasonCounts(plan.Counts.RatedNotApplied.ByReason)
}

func writeSyncPlanArtifacts(planPath string, plan report.SyncPlan, selectedMatchMode matchModeValue, planTSVBase string) error {
	if err := report.WriteJSON(planPath, plan); err != nil {
		return err
	}

	planDir := filepath.Dir(planPath)
	starPath := filepath.Join(planDir, "plan_star.tsv")
	unstarPath := filepath.Join(planDir, "plan_unstar.tsv")
	lovedNotAppliedPath := filepath.Join(planDir, "unapplied_loved.tsv")
	ratedNotAppliedPath := filepath.Join(planDir, "unapplied_rated.tsv")
	if err := report.WriteTSV(starPath, planAuditHeader(), buildPlanStarRows(plan.Loved.WillStar, selectedMatchMode)); err != nil {
		return err
	}
	if err := report.WriteTSV(unstarPath, planAuditHeader(), buildPlanUnstarRows(plan.Unstar.WillUnstar, selectedMatchMode)); err != nil {
		return err
	}
	if err := report.WriteTSV(lovedNotAppliedPath, planAuditHeader(), buildUnappliedLovedRows(plan.Loved.WontStar, selectedMatchMode)); err != nil {
		return err
	}
	if len(plan.Ratings.WontSet) > 0 {
		if err := report.WriteTSV(ratedNotAppliedPath, planAuditHeader(), buildUnappliedRatedRows(plan.Ratings.WontSet, selectedMatchMode)); err != nil {
			return err
		}
	}
	if planTSVBase != "" {
		if err := writePlanTSV(planTSVBase, plan); err != nil {
			return err
		}
	}
	return nil
}

func runReportReconcile(itunesXML string, planPath string, reconcilePath string, filters filterOptions, allowMismatch bool) error {
	if planPath == "" {
		return fmt.Errorf("--report_reconcile requires --report_sync_plan to supply plan counts")
	}
	if reconcilePath == "" {
		return fmt.Errorf("--report_reconcile requires a path")
	}
	stats, err := buildLibraryStats(itunesXML, filters, true)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var plan report.SyncPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return err
	}
	apple := report.AppleDisaggregation{
		TracksTotal:         stats.Tracks.Total,
		TracksLocal:         stats.Tracks.Local,
		TracksRemote:        stats.Tracks.Remote,
		LovedTotal:          stats.Loved.Total,
		LovedLocal:          stats.Loved.Local,
		LovedRemote:         stats.Loved.Remote,
		RatedTotal:          stats.Rated.Total,
		RatedLocal:          stats.Rated.Local,
		RatedRemote:         stats.Rated.Remote,
		LovedAndRatedTotal:  stats.LovedAndRated.Total,
		LovedAndRatedLocal:  stats.LovedAndRated.Local,
		LovedAndRatedRemote: stats.LovedAndRated.Remote,
		LovedOnlyTotal:      stats.LovedOnly.Total,
		LovedOnlyLocal:      stats.LovedOnly.Local,
		LovedOnlyRemote:     stats.LovedOnly.Remote,
		RatedOnlyTotal:      stats.RatedOnly.Total,
		RatedOnlyLocal:      stats.RatedOnly.Local,
		RatedOnlyRemote:     stats.RatedOnly.Remote,
	}
	planCounts := report.PlanCountsSummary{
		PlanStarCount:        plan.Counts.PlannedStar.Total,
		PlanUnstarCount:      plan.Counts.PlannedUnstar,
		PlanRateSetCount:     plan.Counts.PlannedRatingsSet,
		PlanRateUnsetCount:   plan.Counts.PlannedRatingsUnset,
		PlanPlaycountCount:   plan.Counts.PlannedPlaycountUpdates,
		PlanPlaylistOpsCount: len(plan.Playlists.Entries),
	}
	lovedAlreadyStarred := 0
	for _, entry := range plan.Loved.Noop {
		if entry.Reason != reasonAlreadyStarred {
			continue
		}
		if strings.EqualFold(entry.Apple.TrackType, "Remote") {
			continue
		}
		lovedAlreadyStarred++
	}
	unappliedLovedByReason := make(map[string]int)
	unappliedLoved := 0
	for _, entry := range plan.Loved.WontStar {
		if strings.EqualFold(entry.Apple.TrackType, "Remote") {
			continue
		}
		unappliedLoved++
		if entry.NotAppliedReason != "" {
			unappliedLovedByReason[entry.NotAppliedReason]++
		}
	}
	lovedRecon := report.LovedReconcileSummary{
		AppleLovedLocal:                     stats.Loved.Local,
		NavidromeStarredTotal:               plan.NavidromeSummary.StarredTotal,
		LovedAlreadyStarredInNavidromeCount: lovedAlreadyStarred,
		PlanStarCount:                       plan.Counts.PlannedStar.Local,
		PlanUnappliedLovedCount:             unappliedLoved,
		PlanUnappliedLovedByReason:          unappliedLovedByReason,
	}
	reconcile := report.ReconcileReport{
		SchemaVersion:       1,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
		Apple:               apple,
		Navidrome:           plan.NavidromeSummary,
		PlanCounts:          planCounts,
		LovedRecon:          lovedRecon,
		PlanLovedNotApplied: plan.Loved.WontStar,
		PlanRatedNotApplied: plan.Ratings.WontSet,
	}
	expected := stats.Loved.Local
	actual := lovedAlreadyStarred + plan.Counts.PlannedStar.Local + unappliedLoved
	if expected != actual {
		reconcile.ReconcileError = &report.ReconcileError{
			Message:  "apple_loved_local did not match starred + planned + unapplied",
			Expected: expected,
			Actual:   actual,
			Components: map[string]int{
				"loved_already_starred_in_navidrome_count": lovedAlreadyStarred,
				"plan_star_count":                          plan.Counts.PlannedStar.Local,
				"plan_unapplied_loved_count":               unappliedLoved,
			},
		}
	}
	if err := report.WriteJSON(reconcilePath, reconcile); err != nil {
		return err
	}
	if reconcile.ReconcileError != nil && !allowMismatch {
		return fmt.Errorf("reconcile invariant failed: apple_loved_local=%d computed=%d (set --allow_reconcile_mismatch=true to override)", expected, actual)
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

func incrementLovedNotApplied(plan *report.SyncPlan, trackType string, reason string) {
	if strings.EqualFold(trackType, "Remote") {
		return
	}
	plan.Counts.LovedNotApplied.Total++
	plan.Counts.LovedNotApplied.ByReason[reason]++
}

func countRatedNavidrome(songs []navidromeSong) int {
	count := 0
	for _, song := range songs {
		if song.Rating > 0 {
			count++
		}
	}
	return count
}

func printDryRunSummary(stats report.LibraryStats, plan report.SyncPlan) {
	fmt.Fprintln(stdoutWriter, "== Dry Run Summary ==")
	fmt.Fprintf(stdoutWriter, "Apple Loved (local): %d\n", stats.Loved.Local)
	fmt.Fprintf(stdoutWriter, "Apple Loved Only (local): %d\n", stats.LovedOnly.Local)
	fmt.Fprintf(stdoutWriter, "Apple Loved & Rated (local): %d\n", stats.LovedAndRated.Local)
	fmt.Fprintf(stdoutWriter, "Apple Rated Only (local): %d\n", stats.RatedOnly.Local)
	fmt.Fprintf(stdoutWriter, "Navidrome Starred Baseline: %d\n", plan.NavidromeSummary.StarredTotal)
	fmt.Fprintf(stdoutWriter, "Plan: star=%d unstar=%d\n", plan.Counts.PlannedStar.Total, plan.Counts.PlannedUnstar)
	reasonSummary := topReasonSummary(plan.Counts.LovedNotApplied.ByReason, 5)
	if reasonSummary == "" {
		fmt.Fprintf(stdoutWriter, "Unapplied Loved: total=%d (top reasons: none)\n", plan.Counts.LovedNotApplied.Total)
		return
	}
	fmt.Fprintf(stdoutWriter, "Unapplied Loved: total=%d (top reasons: %s)\n", plan.Counts.LovedNotApplied.Total, reasonSummary)
}

func topReasonSummary(counts map[string]int, limit int) string {
	if len(counts) == 0 || limit <= 0 {
		return ""
	}
	type reasonCount struct {
		reason string
		count  int
	}
	reasons := make([]reasonCount, 0, len(counts))
	for reason, count := range counts {
		reasons = append(reasons, reasonCount{reason: reason, count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].count != reasons[j].count {
			return reasons[i].count > reasons[j].count
		}
		return reasons[i].reason < reasons[j].reason
	})
	if len(reasons) > limit {
		reasons = reasons[:limit]
	}
	parts := make([]string, 0, len(reasons))
	for _, entry := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", entry.reason, entry.count))
	}
	return strings.Join(parts, ", ")
}

func sortLovedPlanEntries(entries []report.LovedPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		return compareAppleTrack(a.Apple, b.Apple)
	})
}

func sortRatingPlanEntries(entries []report.RatingPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		return compareAppleTrack(a.Apple, b.Apple)
	})
}

func sortUnstarPlanEntries(entries []report.UnstarPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		return compareNavidromeTrack(a.Navidrome, b.Navidrome)
	})
}

func sortPlayCountPlanEntries(entries []report.PlayCountPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return compareAppleTrack(entries[i].Apple, entries[j].Apple)
	})
}

func sortPlaylistPlanEntries(entries []report.PlaylistPlanEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	for i := range entries {
		sortPlaylistTrackRefs(entries[i].AddTracks)
		sortPlaylistTrackRefs(entries[i].RemoveTracks)
		sortPlaylistTrackRefs(entries[i].MissingTracks)
	}
}

func sortPlaylistTrackRefs(entries []report.PlaylistTrackRef) {
	sort.Slice(entries, func(i, j int) bool {
		a := entries[i]
		b := entries[j]
		if a.Artist != b.Artist {
			return a.Artist < b.Artist
		}
		if a.Album != b.Album {
			return a.Album < b.Album
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.NavidromeSongID != b.NavidromeSongID {
			return a.NavidromeSongID < b.NavidromeSongID
		}
		return a.AppleTrackID < b.AppleTrackID
	})
}

func compareAppleTrack(a report.AppleTrack, b report.AppleTrack) bool {
	if a.Artist != b.Artist {
		return a.Artist < b.Artist
	}
	if a.Album != b.Album {
		return a.Album < b.Album
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.PathClean != b.PathClean {
		return a.PathClean < b.PathClean
	}
	return a.TrackID < b.TrackID
}

func compareNavidromeTrack(a report.NavidromeTrack, b report.NavidromeTrack) bool {
	if a.Artist != b.Artist {
		return a.Artist < b.Artist
	}
	if a.Album != b.Album {
		return a.Album < b.Album
	}
	if a.Title != b.Title {
		return a.Title < b.Title
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	return a.SongID < b.SongID
}

func planAuditHeader() []string {
	return []string{
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
	}
}

func buildPlanStarRows(entries []report.LovedPlanEntry, mode matchModeValue) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		nav := entry.Navidrome
		rows = append(rows, []string{
			"star",
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.SongID }),
			strconv.Itoa(entry.Apple.TrackID),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Artist }), entry.Apple.Artist),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Album }), entry.Apple.Album),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Title }), entry.Apple.Name),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Path }), entry.Apple.PathClean),
			entry.Reason,
			string(mode),
			matchConfidence(entry.NotAppliedReason, nav),
		})
	}
	return rows
}

func buildPlanUnstarRows(entries []report.UnstarPlanEntry, mode matchModeValue) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		appleID := ""
		if entry.Apple != nil {
			appleID = strconv.Itoa(entry.Apple.TrackID)
		}
		rows = append(rows, []string{
			"unstar",
			entry.Navidrome.SongID,
			appleID,
			entry.Navidrome.Artist,
			entry.Navidrome.Album,
			entry.Navidrome.Title,
			entry.Navidrome.Path,
			entry.Reason,
			string(mode),
			"matched",
		})
	}
	return rows
}

func buildUnappliedLovedRows(entries []report.LovedPlanEntry, mode matchModeValue) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(entry.Apple.TrackType, "Remote") {
			continue
		}
		nav := entry.Navidrome
		rows = append(rows, []string{
			"star",
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.SongID }),
			strconv.Itoa(entry.Apple.TrackID),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Artist }), entry.Apple.Artist),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Album }), entry.Apple.Album),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Title }), entry.Apple.Name),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Path }), entry.Apple.PathClean),
			entry.NotAppliedReason,
			string(mode),
			matchConfidence(entry.NotAppliedReason, nav),
		})
	}
	return rows
}

func buildUnappliedRatedRows(entries []report.RatingPlanEntry, mode matchModeValue) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		nav := entry.Navidrome
		rows = append(rows, []string{
			"rate",
			navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.SongID }),
			strconv.Itoa(entry.Apple.TrackID),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Artist }), entry.Apple.Artist),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Album }), entry.Apple.Album),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Title }), entry.Apple.Name),
			firstNonEmpty(navidromeValue(nav, func(n *report.NavidromeTrack) string { return n.Path }), entry.Apple.PathClean),
			entry.NotAppliedReason,
			string(mode),
			matchConfidence(entry.NotAppliedReason, nav),
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

func matchConfidence(reason string, nav *report.NavidromeTrack) string {
	if nav != nil {
		return "matched"
	}
	switch reason {
	case reasonAmbiguousMatchMultiple:
		return "ambiguous"
	default:
		return "unmatched"
	}
}

func ptrNavidromeTrack(track report.NavidromeTrack) *report.NavidromeTrack {
	return &track
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func buildPlaylistPlanEntry(playlist playlistRef, ctx playlistPlanContext, allowlist map[string]struct{}, verifySrcFiles bool, c *subsonic.Client, playlistsByName map[string]*subsonic.Playlist) (report.PlaylistPlanEntry, error) {
	entry := report.PlaylistPlanEntry{
		Operation: "playlist",
		Name:      playlist.Name,
		Action:    "noop",
	}
	desiredIDs := make([]string, 0, len(playlist.Items))
	desiredRefs := make(map[string]report.PlaylistTrackRef)
	missing := make([]report.PlaylistTrackRef, 0)

	for _, item := range playlist.Items {
		if item.TrackId == 0 {
			continue
		}
		info, ok := ctx.appleByID[item.TrackId]
		if !ok {
			missing = append(missing, report.PlaylistTrackRef{
				AppleTrackID: item.TrackId,
				Reason:       "apple_track_missing",
			})
			continue
		}
		reason, matchKey := buildNotAppliedReason(info, allowlist, verifySrcFiles)
		if reason != "" {
			missing = append(missing, playlistMissingRef(info, reason))
			continue
		}
		matches := ctx.navByMatch[matchKey]
		if len(matches) == 0 {
			missing = append(missing, playlistMissingRef(info, reasonNotInNavidromeIndex))
			continue
		}
		if len(matches) > 1 {
			missing = append(missing, playlistMissingRef(info, reasonAmbiguousMatchMultiple))
			continue
		}
		match := matches[0]
		if _, exists := desiredRefs[match.ID]; !exists {
			desiredIDs = append(desiredIDs, match.ID)
			desiredRefs[match.ID] = playlistTrackRef(info, match)
		}
	}

	entry.MissingTracks = missing
	if len(desiredIDs) == 0 {
		entry.Reason = reasonPlaylistNoTracks
		return entry, nil
	}

	existing := playlistsByName[playlist.Name]
	if existing == nil {
		entry.Action = "create"
		entry.AddTracks = refsFromIDs(desiredIDs, desiredRefs, ctx.navidromeByID)
		return entry, nil
	}
	entry.NavidromePlaylistID = existing.ID
	full, err := c.GetPlaylist(existing.ID)
	if err != nil {
		return entry, err
	}
	existingIDs := make([]string, 0, len(full.Entry))
	existingSet := make(map[string]struct{})
	for _, child := range full.Entry {
		if child.ID == "" {
			continue
		}
		existingIDs = append(existingIDs, child.ID)
		existingSet[child.ID] = struct{}{}
	}

	desiredSet := make(map[string]struct{})
	for _, id := range desiredIDs {
		desiredSet[id] = struct{}{}
	}

	addIDs := make([]string, 0)
	for _, id := range desiredIDs {
		if _, ok := existingSet[id]; !ok {
			addIDs = append(addIDs, id)
		}
	}
	removeIDs := make([]string, 0)
	for _, id := range existingIDs {
		if _, ok := desiredSet[id]; !ok {
			removeIDs = append(removeIDs, id)
		}
	}

	if len(addIDs) == 0 && len(removeIDs) == 0 {
		entry.Action = "noop"
		return entry, nil
	}
	entry.Action = "update"
	entry.AddTracks = refsFromIDs(addIDs, desiredRefs, ctx.navidromeByID)
	entry.RemoveTracks = removeRefsFromIDs(removeIDs, ctx.navidromeByID)
	return entry, nil
}

func playlistTrackRef(info appleTrackInfo, match navidromeSong) report.PlaylistTrackRef {
	return report.PlaylistTrackRef{
		AppleTrackID:    info.track.TrackId,
		NavidromeSongID: match.ID,
		Title:           firstNonEmpty(match.Title, info.track.Name),
		Artist:          firstNonEmpty(match.Artist, info.track.Artist),
		Album:           firstNonEmpty(match.Album, info.track.Album),
		Path:            firstNonEmpty(match.Path, info.location.parsed),
	}
}

func playlistMissingRef(info appleTrackInfo, reason string) report.PlaylistTrackRef {
	return report.PlaylistTrackRef{
		AppleTrackID: info.track.TrackId,
		Title:        info.track.Name,
		Artist:       info.track.Artist,
		Album:        info.track.Album,
		Path:         info.location.parsed,
		Reason:       reason,
	}
}

func refsFromIDs(ids []string, desired map[string]report.PlaylistTrackRef, navidromeByID map[string]navidromeSong) []report.PlaylistTrackRef {
	result := make([]report.PlaylistTrackRef, 0, len(ids))
	for _, id := range ids {
		if ref, ok := desired[id]; ok {
			result = append(result, ref)
			continue
		}
		if nav, ok := navidromeByID[id]; ok {
			result = append(result, report.PlaylistTrackRef{
				NavidromeSongID: nav.ID,
				Title:           nav.Title,
				Artist:          nav.Artist,
				Album:           nav.Album,
				Path:            nav.Path,
			})
			continue
		}
		result = append(result, report.PlaylistTrackRef{NavidromeSongID: id})
	}
	return result
}

func removeRefsFromIDs(ids []string, navidromeByID map[string]navidromeSong) []report.PlaylistTrackRef {
	result := make([]report.PlaylistTrackRef, 0, len(ids))
	for _, id := range ids {
		if nav, ok := navidromeByID[id]; ok {
			result = append(result, report.PlaylistTrackRef{
				NavidromeSongID: nav.ID,
				Title:           nav.Title,
				Artist:          nav.Artist,
				Album:           nav.Album,
				Path:            nav.Path,
			})
			continue
		}
		result = append(result, report.PlaylistTrackRef{NavidromeSongID: id})
	}
	return result
}

func writePlanTSV(basePath string, plan report.SyncPlan) error {
	dir := filepath.Dir(basePath)
	base := strings.TrimSuffix(filepath.Base(basePath), filepath.Ext(basePath))
	prefix := filepath.Join(dir, base)

	if err := report.WriteTSV(prefix+"_stars.tsv", []string{
		"operation", "action", "reason",
		"apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_track_type", "apple_rating", "apple_loved", "apple_path",
		"navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path",
	}, buildStarPlanRows(plan)); err != nil {
		return err
	}
	if err := report.WriteTSV(prefix+"_ratings.tsv", []string{
		"operation", "action", "reason",
		"apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_track_type", "apple_rating", "apple_loved", "apple_path",
		"desired_rating", "navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path", "navidrome_rating",
	}, buildRatingPlanRows(plan)); err != nil {
		return err
	}
	if err := report.WriteTSV(prefix+"_playcounts.tsv", []string{
		"operation", "action", "reason",
		"apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_play_count", "apple_last_played", "apple_path",
		"desired_scrobble_count", "navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path", "navidrome_play_count",
	}, buildPlaycountPlanRows(plan)); err != nil {
		return err
	}
	if err := report.WriteTSV(prefix+"_unstar.tsv", []string{
		"operation", "action", "reason",
		"navidrome_song_id", "navidrome_title", "navidrome_artist", "navidrome_album", "navidrome_path",
		"apple_track_id", "apple_name", "apple_artist", "apple_album", "apple_path",
	}, buildUnstarPlanRows(plan)); err != nil {
		return err
	}
	if err := report.WriteTSV(prefix+"_playlists.tsv", []string{
		"operation", "playlist_action", "playlist_name", "playlist_id", "track_action",
		"apple_track_id", "navidrome_song_id", "title", "artist", "album", "path", "reason",
	}, buildPlaylistPlanRows(plan)); err != nil {
		return err
	}
	return nil
}

func buildStarPlanRows(plan report.SyncPlan) [][]string {
	rows := make([][]string, 0)
	appendEntry := func(entry report.LovedPlanEntry) {
		reason := entry.Reason
		if entry.NotAppliedReason != "" {
			reason = entry.NotAppliedReason
		}
		rows = append(rows, []string{
			entry.Operation,
			entry.Action,
			reason,
			strconv.Itoa(entry.Apple.TrackID),
			entry.Apple.Name,
			entry.Apple.Artist,
			entry.Apple.Album,
			entry.Apple.TrackType,
			strconv.Itoa(entry.Apple.Rating),
			strconv.FormatBool(entry.Apple.Loved),
			entry.Apple.PathClean,
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.SongID }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Title }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Artist }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Album }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Path }),
		})
	}
	for _, entry := range plan.Loved.WillStar {
		appendEntry(entry)
	}
	for _, entry := range plan.Loved.Noop {
		appendEntry(entry)
	}
	for _, entry := range plan.Loved.WontStar {
		appendEntry(entry)
	}
	return rows
}

func buildRatingPlanRows(plan report.SyncPlan) [][]string {
	rows := make([][]string, 0)
	appendEntry := func(entry report.RatingPlanEntry) {
		reason := entry.Reason
		if entry.NotAppliedReason != "" {
			reason = entry.NotAppliedReason
		}
		rows = append(rows, []string{
			entry.Operation,
			entry.Action,
			reason,
			strconv.Itoa(entry.Apple.TrackID),
			entry.Apple.Name,
			entry.Apple.Artist,
			entry.Apple.Album,
			entry.Apple.TrackType,
			strconv.Itoa(entry.Apple.Rating),
			strconv.FormatBool(entry.Apple.Loved),
			entry.Apple.PathClean,
			strconv.Itoa(entry.DesiredRating),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.SongID }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Title }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Artist }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Album }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Path }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return strconv.Itoa(n.Rating) }),
		})
	}
	for _, entry := range plan.Ratings.WillSet {
		appendEntry(entry)
	}
	for _, entry := range plan.Ratings.WillUnset {
		appendEntry(entry)
	}
	for _, entry := range plan.Ratings.Noop {
		appendEntry(entry)
	}
	for _, entry := range plan.Ratings.WontSet {
		appendEntry(entry)
	}
	return rows
}

func buildPlaycountPlanRows(plan report.SyncPlan) [][]string {
	rows := make([][]string, 0)
	appendEntry := func(entry report.PlayCountPlanEntry) {
		rows = append(rows, []string{
			entry.Operation,
			entry.Action,
			entry.Reason,
			strconv.Itoa(entry.Apple.TrackID),
			entry.Apple.Name,
			entry.Apple.Artist,
			entry.Apple.Album,
			strconv.Itoa(entry.ApplePlayCount),
			entry.AppleLastPlayed,
			entry.Apple.PathClean,
			strconv.FormatInt(entry.DesiredScrobbleCount, 10),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.SongID }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Title }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Artist }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Album }),
			navidromeValue(entry.Navidrome, func(n *report.NavidromeTrack) string { return n.Path }),
			strconv.FormatInt(entry.NavidromePlayCount, 10),
		})
	}
	for _, entry := range plan.PlayCount.WillUpdate {
		appendEntry(entry)
	}
	for _, entry := range plan.PlayCount.Noop {
		appendEntry(entry)
	}
	for _, entry := range plan.PlayCount.WontUpdate {
		appendEntry(entry)
	}
	return rows
}

func buildUnstarPlanRows(plan report.SyncPlan) [][]string {
	rows := make([][]string, 0)
	appendEntry := func(entry report.UnstarPlanEntry) {
		apple := entry.Apple
		appleID := ""
		appleName := ""
		appleArtist := ""
		appleAlbum := ""
		applePath := ""
		if apple != nil {
			appleID = strconv.Itoa(apple.TrackID)
			appleName = apple.Name
			appleArtist = apple.Artist
			appleAlbum = apple.Album
			applePath = apple.PathClean
		}
		rows = append(rows, []string{
			entry.Operation,
			entry.Action,
			entry.Reason,
			entry.Navidrome.SongID,
			entry.Navidrome.Title,
			entry.Navidrome.Artist,
			entry.Navidrome.Album,
			entry.Navidrome.Path,
			appleID,
			appleName,
			appleArtist,
			appleAlbum,
			applePath,
		})
	}
	for _, entry := range plan.Unstar.WillUnstar {
		appendEntry(entry)
	}
	for _, entry := range plan.Unstar.WontUnstar {
		appendEntry(entry)
	}
	return rows
}

func buildPlaylistPlanRows(plan report.SyncPlan) [][]string {
	rows := make([][]string, 0)
	for _, entry := range plan.Playlists.Entries {
		if len(entry.AddTracks) == 0 && len(entry.RemoveTracks) == 0 && len(entry.MissingTracks) == 0 {
			rows = append(rows, []string{
				entry.Operation,
				entry.Action,
				entry.Name,
				entry.NavidromePlaylistID,
				"summary",
				"",
				"",
				"",
				"",
				"",
				"",
				entry.Reason,
			})
			continue
		}
		appendTrack := func(trackAction string, track report.PlaylistTrackRef) {
			rows = append(rows, []string{
				entry.Operation,
				entry.Action,
				entry.Name,
				entry.NavidromePlaylistID,
				trackAction,
				strconv.Itoa(track.AppleTrackID),
				track.NavidromeSongID,
				track.Title,
				track.Artist,
				track.Album,
				track.Path,
				track.Reason,
			})
		}
		for _, track := range entry.AddTracks {
			appendTrack("add", track)
		}
		for _, track := range entry.RemoveTracks {
			appendTrack("remove", track)
		}
		for _, track := range entry.MissingTracks {
			appendTrack("missing", track)
		}
	}
	return rows
}
