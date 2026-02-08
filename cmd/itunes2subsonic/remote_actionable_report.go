package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/logank/itunes2subsonic/internal/report"
)

type localTrackMeta struct {
	Loved  bool
	Rating int
}

type remoteActionableRow struct {
	AppleTrackID      int
	ApplePersistentID string
	RemoteLoved       bool
	RemoteRating      int
	LocalLoved        bool
	LocalRating       int
	RemoteArtist      string
	RemoteAlbum       string
	RemoteTitle       string
	MatchedSongID     string
	MatchedPath       string
	MatchStatus       report.RemoteMatchStatus
	MatchScore        float64
	MatchMethod       string
	CandidateCount    int
}

type remoteActionableSummary struct {
	Total              int
	LovedOnly          int
	RatedOnly          int
	LovedAndRated      int
	MatchCount         int
	LowConfidenceCount int
}

func buildLocalMetaIndex(appleTracks []appleTrackInfo) map[int]localTrackMeta {
	index := make(map[int]localTrackMeta, len(appleTracks))
	for _, info := range appleTracks {
		index[info.track.TrackId] = localTrackMeta{
			Loved:  info.loved,
			Rating: info.track.Rating,
		}
	}
	return index
}

func buildRemoteActionableReport(entries []report.RemoteMatchEntry, localIndex map[int]localTrackMeta, includeLow bool) ([]remoteActionableRow, remoteActionableSummary) {
	rows := make([]remoteActionableRow, 0)
	summary := remoteActionableSummary{}
	for _, entry := range entries {
		if !isRemoteMatchStatusAllowed(entry.MatchStatus, includeLow) {
			continue
		}
		hasRemoteLoved := entry.Loved
		hasRemoteRating := entry.Rating > 0
		if !hasRemoteLoved && !hasRemoteRating {
			continue
		}
		local := localIndex[entry.AppleTrackID]
		needsLoved := hasRemoteLoved && !local.Loved
		needsRating := hasRemoteRating && (local.Rating == 0 || local.Rating != entry.Rating)
		if !needsLoved && !needsRating {
			continue
		}
		rows = append(rows, remoteActionableRow{
			AppleTrackID:      entry.AppleTrackID,
			ApplePersistentID: entry.ApplePersistentID,
			RemoteLoved:       entry.Loved,
			RemoteRating:      entry.Rating,
			LocalLoved:        local.Loved,
			LocalRating:       local.Rating,
			RemoteArtist:      entry.Artist,
			RemoteAlbum:       entry.Album,
			RemoteTitle:       entry.Title,
			MatchedSongID:     entry.MatchedSongID,
			MatchedPath:       entry.MatchedPath,
			MatchStatus:       entry.MatchStatus,
			MatchScore:        entry.MatchScore,
			MatchMethod:       entry.MatchMethod,
			CandidateCount:    entry.CandidateCount,
		})
		summary.Total++
		switch entry.MatchStatus {
		case report.RemoteMatchStatusMatch:
			summary.MatchCount++
		case report.RemoteMatchStatusLowConfidence:
			summary.LowConfidenceCount++
		}
		switch {
		case hasRemoteLoved && hasRemoteRating:
			summary.LovedAndRated++
		case hasRemoteLoved:
			summary.LovedOnly++
		case hasRemoteRating:
			summary.RatedOnly++
		}
	}
	return rows, summary
}

func remoteActionableTSVHeader() []string {
	return []string{
		"apple_track_id",
		"apple_persistent_id",
		"remote_loved",
		"remote_rating",
		"local_loved",
		"local_rating",
		"remote_artist",
		"remote_album",
		"remote_title",
		"matched_navidrome_song_id",
		"matched_path",
		"match_status",
		"match_score",
		"match_method",
		"candidate_count",
	}
}

func remoteActionableTSVRows(rows []remoteActionableRow) [][]string {
	result := make([][]string, 0, len(rows))
	for _, row := range rows {
		remoteRating := ""
		if row.RemoteRating > 0 {
			remoteRating = fmt.Sprintf("%d", row.RemoteRating)
		}
		localRating := ""
		if row.LocalRating > 0 {
			localRating = fmt.Sprintf("%d", row.LocalRating)
		}
		matchScore := ""
		if row.MatchScore > 0 {
			matchScore = fmt.Sprintf("%.4f", row.MatchScore)
		}
		result = append(result, []string{
			fmt.Sprintf("%d", row.AppleTrackID),
			row.ApplePersistentID,
			fmt.Sprintf("%t", row.RemoteLoved),
			remoteRating,
			fmt.Sprintf("%t", row.LocalLoved),
			localRating,
			row.RemoteArtist,
			row.RemoteAlbum,
			row.RemoteTitle,
			row.MatchedSongID,
			row.MatchedPath,
			string(row.MatchStatus),
			matchScore,
			row.MatchMethod,
			fmt.Sprintf("%d", row.CandidateCount),
		})
	}
	return result
}

func isRemoteMatchStatusAllowed(status report.RemoteMatchStatus, includeLow bool) bool {
	if status == report.RemoteMatchStatusMatch {
		return true
	}
	return includeLow && status == report.RemoteMatchStatusLowConfidence
}

func parseRemoteMatchTSV(reader io.Reader) ([]report.RemoteMatchEntry, error) {
	csvReader := csv.NewReader(reader)
	csvReader.Comma = '\t'
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true

	header, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	cols := map[string]int{}
	for i, name := range header {
		cols[strings.TrimSpace(name)] = i
	}

	getIndex := func(name string) (int, error) {
		idx, ok := cols[name]
		if !ok {
			return 0, fmt.Errorf("missing column %q", name)
		}
		return idx, nil
	}

	appleTrackIDCol, err := getIndex("apple_track_id")
	if err != nil {
		return nil, err
	}
	applePersistentCol, err := getIndex("apple_persistent_id")
	if err != nil {
		return nil, err
	}
	lovedCol, err := getIndex("loved")
	if err != nil {
		return nil, err
	}
	ratingCol, err := getIndex("rating")
	if err != nil {
		return nil, err
	}
	artistCol, err := getIndex("artist")
	if err != nil {
		return nil, err
	}
	albumCol, err := getIndex("album")
	if err != nil {
		return nil, err
	}
	titleCol, err := getIndex("title")
	if err != nil {
		return nil, err
	}
	statusCol, err := getIndex("match_status")
	if err != nil {
		return nil, err
	}
	matchedSongCol, err := getIndex("matched_navidrome_song_id")
	if err != nil {
		return nil, err
	}
	matchedPathCol, err := getIndex("matched_path")
	if err != nil {
		return nil, err
	}
	matchScoreCol, err := getIndex("match_score")
	if err != nil {
		return nil, err
	}
	matchMethodCol, err := getIndex("match_method")
	if err != nil {
		return nil, err
	}
	candidateCountCol, err := getIndex("candidate_count")
	if err != nil {
		return nil, err
	}

	entries := make([]report.RemoteMatchEntry, 0)
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		trackID, err := strconv.Atoi(strings.TrimSpace(record[appleTrackIDCol]))
		if err != nil {
			return nil, err
		}
		loved := false
		if value := strings.TrimSpace(record[lovedCol]); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, err
			}
			loved = parsed
		}
		rating := 0
		if value := strings.TrimSpace(record[ratingCol]); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			rating = parsed
		}
		matchScore := 0.0
		if value := strings.TrimSpace(record[matchScoreCol]); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, err
			}
			matchScore = parsed
		}
		candidateCount := 0
		if value := strings.TrimSpace(record[candidateCountCol]); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			candidateCount = parsed
		}
		entries = append(entries, report.RemoteMatchEntry{
			AppleTrackID:      trackID,
			ApplePersistentID: strings.TrimSpace(record[applePersistentCol]),
			Loved:             loved,
			Rating:            rating,
			Artist:            strings.TrimSpace(record[artistCol]),
			Album:             strings.TrimSpace(record[albumCol]),
			Title:             strings.TrimSpace(record[titleCol]),
			MatchStatus:       report.RemoteMatchStatus(strings.TrimSpace(record[statusCol])),
			MatchedSongID:     strings.TrimSpace(record[matchedSongCol]),
			MatchedPath:       strings.TrimSpace(record[matchedPathCol]),
			MatchScore:        matchScore,
			MatchMethod:       strings.TrimSpace(record[matchMethodCol]),
			CandidateCount:    candidateCount,
		})
	}

	return entries, nil
}
