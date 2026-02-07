package main

import (
	"sort"
	"strconv"
	"time"

	"github.com/logank/itunes2subsonic/internal/report"
)

const (
	notAppliedSummarySchemaVersion = 1
	notAppliedRowLimit             = 5000
	notAppliedSampleLimit          = 5
	notAppliedUnknownReason        = "unknown"
)

type notAppliedBundle struct {
	RowsByDomain map[report.NotAppliedDomain][]report.NotAppliedRow
	RowsAll      []report.NotAppliedRow
	Summary      report.NotAppliedSummary
}

func buildNotAppliedBundle(plan report.SyncPlan, generatedAt time.Time) notAppliedBundle {
	rowsByDomain := map[report.NotAppliedDomain][]report.NotAppliedRow{
		report.NotAppliedDomainStars:      {},
		report.NotAppliedDomainRatings:    {},
		report.NotAppliedDomainPlaycounts: {},
		report.NotAppliedDomainPlaylists:  {},
	}
	addRow := func(row report.NotAppliedRow) {
		if row.Reason == "" {
			row.Reason = notAppliedUnknownReason
		}
		rowsByDomain[row.Domain] = append(rowsByDomain[row.Domain], row)
	}
	for _, entry := range plan.Loved.WontStar {
		reason := entry.NotAppliedReason
		if reason == "" {
			reason = entry.Reason
		}
		addRow(notAppliedRowFromApple(report.NotAppliedDomainStars, reason, entry.Apple, entry.Navidrome, ""))
	}
	for _, entry := range plan.Ratings.WontSet {
		reason := entry.NotAppliedReason
		if reason == "" {
			reason = entry.Reason
		}
		addRow(notAppliedRowFromApple(report.NotAppliedDomainRatings, reason, entry.Apple, entry.Navidrome, ""))
	}
	for _, entry := range plan.PlayCount.WontUpdate {
		addRow(notAppliedRowFromApple(report.NotAppliedDomainPlaycounts, entry.Reason, entry.Apple, entry.Navidrome, ""))
	}
	for _, entry := range plan.Playlists.Entries {
		for _, track := range entry.MissingTracks {
			addRow(report.NotAppliedRow{
				Domain:       report.NotAppliedDomainPlaylists,
				Reason:       firstNonEmpty(track.Reason, notAppliedUnknownReason),
				AppleTrackID: track.AppleTrackID,
				AppleName:    track.Title,
				AppleArtist:  track.Artist,
				AppleAlbum:   track.Album,
				ApplePath:    track.Path,
				ApplePathRaw: track.PathRaw,
				PlaylistName: entry.Name,
			})
		}
	}

	rowsAll := make([]report.NotAppliedRow, 0)
	for domain := range rowsByDomain {
		rowsByDomain[domain] = sortNotAppliedRows(rowsByDomain[domain])
		rowsAll = append(rowsAll, rowsByDomain[domain]...)
	}
	rowsAll = sortNotAppliedRows(rowsAll)

	summary := buildNotAppliedSummary(rowsByDomain, rowsAll, generatedAt)
	return notAppliedBundle{
		RowsByDomain: rowsByDomain,
		RowsAll:      rowsAll,
		Summary:      summary,
	}
}

func notAppliedRowFromApple(domain report.NotAppliedDomain, reason string, apple report.AppleTrack, navidrome *report.NavidromeTrack, playlistName string) report.NotAppliedRow {
	row := report.NotAppliedRow{
		Domain:         domain,
		Reason:         reason,
		AppleTrackID:   apple.TrackID,
		AppleName:      apple.Name,
		AppleArtist:    apple.Artist,
		AppleAlbum:     apple.Album,
		AppleTrackType: apple.TrackType,
		ApplePath:      apple.PathClean,
		ApplePathRaw:   apple.PathRaw,
		PlaylistName:   playlistName,
	}
	if navidrome != nil {
		row.NavidromeSongID = navidrome.SongID
		row.NavidromeTitle = navidrome.Title
		row.NavidromeArtist = navidrome.Artist
		row.NavidromeAlbum = navidrome.Album
		row.NavidromePath = navidrome.Path
	}
	return row
}

func buildNotAppliedSummary(rowsByDomain map[report.NotAppliedDomain][]report.NotAppliedRow, rowsAll []report.NotAppliedRow, generatedAt time.Time) report.NotAppliedSummary {
	byDomain := make(map[report.NotAppliedDomain]report.NotAppliedDomainSummary)
	aggregate := make(map[string]int)
	samples := make(map[report.NotAppliedDomain][]report.NotAppliedRow)
	total := 0
	for domain, rows := range rowsByDomain {
		domainCounts := report.NotAppliedDomainSummary{
			Total:    len(rows),
			ByReason: make(map[string]int),
		}
		for _, row := range rows {
			domainCounts.ByReason[row.Reason]++
			aggregate[row.Reason]++
		}
		byDomain[domain] = domainCounts
		if len(rows) > 0 {
			sampleLimit := notAppliedSampleLimit
			if len(rows) < sampleLimit {
				sampleLimit = len(rows)
			}
			samples[domain] = append([]report.NotAppliedRow(nil), rows[:sampleLimit]...)
		}
		total += len(rows)
	}
	if len(rowsAll) > total {
		total = len(rowsAll)
	}
	return report.NotAppliedSummary{
		SchemaVersion:     notAppliedSummarySchemaVersion,
		GeneratedAt:       generatedAt.UTC().Format(time.RFC3339),
		TotalRows:         total,
		ByDomain:          byDomain,
		AggregateByReason: aggregate,
		SamplesByDomain:   samples,
	}
}

func sortNotAppliedRows(rows []report.NotAppliedRow) []report.NotAppliedRow {
	sort.SliceStable(rows, func(i, j int) bool {
		a := rows[i]
		b := rows[j]
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Reason != b.Reason {
			return a.Reason < b.Reason
		}
		if a.AppleArtist != b.AppleArtist {
			return a.AppleArtist < b.AppleArtist
		}
		if a.AppleAlbum != b.AppleAlbum {
			return a.AppleAlbum < b.AppleAlbum
		}
		if a.AppleName != b.AppleName {
			return a.AppleName < b.AppleName
		}
		if a.ApplePath != b.ApplePath {
			return a.ApplePath < b.ApplePath
		}
		if a.AppleTrackID != b.AppleTrackID {
			return a.AppleTrackID < b.AppleTrackID
		}
		if a.NavidromeSongID != b.NavidromeSongID {
			return a.NavidromeSongID < b.NavidromeSongID
		}
		if a.PlaylistName != b.PlaylistName {
			return a.PlaylistName < b.PlaylistName
		}
		return a.ApplePathRaw < b.ApplePathRaw
	})
	return rows
}

func notAppliedTSVHeader() []string {
	return []string{
		"domain",
		"reason",
		"apple_track_id",
		"apple_name",
		"apple_artist",
		"apple_album",
		"apple_track_type",
		"apple_path",
		"apple_path_raw",
		"navidrome_song_id",
		"navidrome_title",
		"navidrome_artist",
		"navidrome_album",
		"navidrome_path",
		"playlist_name",
	}
}

func buildNotAppliedTSVRows(rows []report.NotAppliedRow) [][]string {
	result := make([][]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, []string{
			string(row.Domain),
			row.Reason,
			strconv.Itoa(row.AppleTrackID),
			row.AppleName,
			row.AppleArtist,
			row.AppleAlbum,
			row.AppleTrackType,
			row.ApplePath,
			row.ApplePathRaw,
			row.NavidromeSongID,
			row.NavidromeTitle,
			row.NavidromeArtist,
			row.NavidromeAlbum,
			row.NavidromePath,
			row.PlaylistName,
		})
	}
	return result
}
