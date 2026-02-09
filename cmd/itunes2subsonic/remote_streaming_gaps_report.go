package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/delucks/go-subsonic"
	i2s "github.com/logank/itunes2subsonic"
	"github.com/logank/itunes2subsonic/internal/itunes"
	"github.com/logank/itunes2subsonic/internal/report"
	"github.com/logank/itunes2subsonic/internal/reporting"
	pb "github.com/schollz/progressbar/v3"
)

const remoteStreamingGapBaseName = "remote_streaming_gaps"

type remoteStreamingGapPaths struct {
	baseDir                   string
	reportJSON                string
	reportTSV                 string
	missingInNavidromeTSV     string
	presentMissingMetadataTSV string
	ambiguousTSV              string
	alignedTSV                string
	summaryJSON               string
}

func runReportRemoteStreamingGaps(c *subsonic.Client, itunesXML string, navidromeDump string, outputDir string) error {
	if itunesXML == "" {
		return fmt.Errorf("--itunes_xml is required")
	}
	if outputDir == "" {
		return fmt.Errorf("--report_remote_streaming_gaps requires a base directory path")
	}
	paths := buildRemoteStreamingGapPaths(outputDir)
	appleTracks, err := loadRemoteLovedRatedTracks(itunesXML)
	if err != nil {
		return err
	}
	navidromeTracks, err := loadNavidromeStreamingTracks(c, navidromeDump)
	if err != nil {
		return err
	}

	reportData := reporting.BuildRemoteStreamingGapReport(buildVersion(), appleTracks, navidromeTracks)
	if err := report.WriteJSON(paths.reportJSON, reportData); err != nil {
		return err
	}
	if err := report.WriteJSON(paths.summaryJSON, reportData.Summary); err != nil {
		return err
	}
	if err := report.WriteTSV(paths.reportTSV, remoteStreamingGapTSVHeader(), remoteStreamingGapTSVRows(reportData.Entries)); err != nil {
		return err
	}
	if err := report.WriteTSV(paths.missingInNavidromeTSV, remoteStreamingGapTSVHeader(), remoteStreamingGapTSVRows(filterRemoteStreamingEntries(reportData.Entries, isMissingInNavidrome))); err != nil {
		return err
	}
	if err := report.WriteTSV(paths.presentMissingMetadataTSV, remoteStreamingGapTSVHeader(), remoteStreamingGapTSVRows(filterRemoteStreamingEntries(reportData.Entries, isPresentButMissingMetadata))); err != nil {
		return err
	}
	if err := report.WriteTSV(paths.ambiguousTSV, remoteStreamingGapTSVHeader(), remoteStreamingGapTSVRows(filterRemoteStreamingEntries(reportData.Entries, isAmbiguousEntry))); err != nil {
		return err
	}
	if err := report.WriteTSV(paths.alignedTSV, remoteStreamingGapTSVHeader(), remoteStreamingGapTSVRows(filterRemoteStreamingEntries(reportData.Entries, isAlignedEntry))); err != nil {
		return err
	}
	return nil
}

func buildRemoteStreamingGapPaths(baseDir string) remoteStreamingGapPaths {
	return remoteStreamingGapPaths{
		baseDir:                   baseDir,
		reportJSON:                filepath.Join(baseDir, remoteStreamingGapBaseName+".json"),
		reportTSV:                 filepath.Join(baseDir, remoteStreamingGapBaseName+".tsv"),
		missingInNavidromeTSV:     filepath.Join(baseDir, remoteStreamingGapBaseName+".missing_in_navidrome.tsv"),
		presentMissingMetadataTSV: filepath.Join(baseDir, remoteStreamingGapBaseName+".present_but_missing_metadata.tsv"),
		ambiguousTSV:              filepath.Join(baseDir, remoteStreamingGapBaseName+".ambiguous.tsv"),
		alignedTSV:                filepath.Join(baseDir, remoteStreamingGapBaseName+".aligned.tsv"),
		summaryJSON:               filepath.Join(baseDir, remoteStreamingGapBaseName+".summary.json"),
	}
}

func loadRemoteLovedRatedTracks(itunesXML string) ([]reporting.RemoteStreamingAppleTrack, error) {
	file, err := os.Open(itunesXML)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	library, err := itunes.LoadLibrary(file)
	if err != nil {
		return nil, err
	}

	tracks := make([]reporting.RemoteStreamingAppleTrack, 0)
	for _, track := range library.Tracks {
		if !isRemoteTrack(track) {
			continue
		}
		loved := isLovedTrack(track)
		if !loved && track.Rating <= 0 {
			continue
		}
		durationSeconds := 0
		if track.TotalTime > 0 {
			durationSeconds = int(math.Round(float64(track.TotalTime) / 1000.0))
		}
		tracks = append(tracks, reporting.RemoteStreamingAppleTrack{
			TrackID:         track.TrackId,
			Title:           track.Name,
			Artist:          track.Artist,
			Album:           track.Album,
			Rating:          track.Rating,
			Loved:           loved,
			TrackNumber:     track.TrackNumber,
			DiscNumber:      track.DiscNumber,
			DurationSeconds: durationSeconds,
			Year:            track.Year,
		})
	}
	return tracks, nil
}

func loadNavidromeStreamingTracks(c *subsonic.Client, navidromeDump string) ([]reporting.RemoteStreamingNavidromeTrack, error) {
	if navidromeDump != "" {
		entries, err := loadNavidromeDump(navidromeDump)
		if err != nil {
			return nil, err
		}
		tracks := make([]reporting.RemoteStreamingNavidromeTrack, 0, len(entries))
		for _, entry := range entries {
			tracks = append(tracks, reporting.RemoteStreamingNavidromeTrack{
				SongID:          entry.ID,
				Title:           entry.Title,
				Artist:          entry.Artist,
				Album:           entry.Album,
				Rating:          entry.Rating,
				Starred:         entry.Starred,
				TrackNumber:     entry.TrackNumber,
				DiscNumber:      entry.DiscNumber,
				DurationSeconds: entry.DurationSeconds,
				Year:            entry.Year,
			})
		}
		return tracks, nil
	}
	if c == nil {
		return nil, fmt.Errorf("Navidrome client is required when --navidrome_dump is not provided")
	}
	bar := i2s.PbWithOptions(pb.Default(-1, "fetching navidrome data"))
	songs, err := fetchSubsonicSongs(c, bar)
	if err != nil {
		return nil, err
	}
	starredSongs, err := fetchStarredSongs(c)
	if err != nil {
		return nil, err
	}
	starredByID := make(map[string]struct{}, len(starredSongs))
	for _, song := range starredSongs {
		starredByID[song.ID] = struct{}{}
	}
	for i := range songs {
		if _, ok := starredByID[songs[i].Id()]; ok {
			songs[i].starred = true
		}
	}
	tracks := make([]reporting.RemoteStreamingNavidromeTrack, 0, len(songs))
	for _, song := range songs {
		tracks = append(tracks, reporting.RemoteStreamingNavidromeTrack{
			SongID:          song.Id(),
			Title:           song.title,
			Artist:          song.artist,
			Album:           song.album,
			Rating:          song.rating,
			Starred:         song.starred,
			TrackNumber:     song.trackNumber,
			DiscNumber:      song.discNumber,
			DurationSeconds: song.durationSeconds,
			Year:            song.year,
		})
	}
	return tracks, nil
}

func remoteStreamingGapTSVHeader() []string {
	return []string{
		"apple_track_id",
		"apple_title",
		"apple_artist",
		"apple_album",
		"apple_rating_5",
		"apple_loved_bool",
		"match_status",
		"navidrome_song_id",
		"navidrome_title",
		"navidrome_artist",
		"navidrome_album",
		"navidrome_rating_100",
		"navidrome_starred_bool",
		"gap_flags",
		"score_best",
		"score_second",
	}
}

func remoteStreamingGapTSVRows(entries []report.RemoteStreamingGapEntry) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		appleRating := ""
		if entry.AppleRating5 > 0 {
			appleRating = strconv.Itoa(entry.AppleRating5)
		}
		navRating := ""
		if entry.NavidromeRating100 > 0 {
			navRating = strconv.Itoa(entry.NavidromeRating100)
		}
		navStarred := ""
		if entry.NavidromeSongID != "" {
			navStarred = strconv.FormatBool(entry.NavidromeStarred)
		}
		scoreBest := ""
		if entry.ScoreBest > 0 {
			scoreBest = fmt.Sprintf("%.4f", entry.ScoreBest)
		}
		scoreSecond := ""
		if entry.ScoreSecond > 0 {
			scoreSecond = fmt.Sprintf("%.4f", entry.ScoreSecond)
		}
		rows = append(rows, []string{
			strconv.Itoa(entry.AppleTrackID),
			entry.AppleTitle,
			entry.AppleArtist,
			entry.AppleAlbum,
			appleRating,
			strconv.FormatBool(entry.AppleLoved),
			string(entry.MatchStatus),
			entry.NavidromeSongID,
			entry.NavidromeTitle,
			entry.NavidromeArtist,
			entry.NavidromeAlbum,
			navRating,
			navStarred,
			strings.Join(entry.GapFlags, ","),
			scoreBest,
			scoreSecond,
		})
	}
	return rows
}

func filterRemoteStreamingEntries(entries []report.RemoteStreamingGapEntry, predicate func(report.RemoteStreamingGapEntry) bool) []report.RemoteStreamingGapEntry {
	filtered := make([]report.RemoteStreamingGapEntry, 0)
	for _, entry := range entries {
		if predicate(entry) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func isMissingInNavidrome(entry report.RemoteStreamingGapEntry) bool {
	return entry.MatchStatus == report.RemoteStreamingGapStatusNoMatch
}

func isPresentButMissingMetadata(entry report.RemoteStreamingGapEntry) bool {
	if entry.MatchStatus != report.RemoteStreamingGapStatusMatch {
		return false
	}
	for _, flag := range entry.GapFlags {
		if flag == "loved_not_starred" || flag == "rating_diff" || flag == "rating_missing" {
			return true
		}
	}
	return false
}

func isAmbiguousEntry(entry report.RemoteStreamingGapEntry) bool {
	return entry.MatchStatus == report.RemoteStreamingGapStatusAmbiguous
}

func isAlignedEntry(entry report.RemoteStreamingGapEntry) bool {
	return entry.MatchStatus == report.RemoteStreamingGapStatusMatch && len(entry.GapFlags) == 0
}
