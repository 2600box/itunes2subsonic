package main

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/logank/itunes2subsonic/internal/report"
	pkgreport "github.com/logank/itunes2subsonic/pkg/report"
)

func TestBuildRemoteActionableReportFilters(t *testing.T) {
	header := pkgreport.TSVHeaderRemoteMatch()
	rows := []map[string]string{
		{
			"apple_track_id":            "1",
			"apple_persistent_id":       "PID1",
			"loved":                     "true",
			"rating":                    "",
			"artist":                    "Artist1",
			"album":                     "Album1",
			"title":                     "Title1",
			"match_status":              string(report.RemoteMatchStatusMatch),
			"matched_navidrome_song_id": "song1",
			"matched_path":              "/music/one.mp3",
			"match_score":               "0.9900",
			"match_method":              "path",
			"candidate_count":           "1",
		},
		{
			"apple_track_id":            "2",
			"apple_persistent_id":       "PID2",
			"loved":                     "true",
			"rating":                    "80",
			"artist":                    "Artist2",
			"album":                     "Album2",
			"title":                     "Title2",
			"match_status":              string(report.RemoteMatchStatusMatch),
			"matched_navidrome_song_id": "song2",
			"matched_path":              "/music/two.mp3",
			"match_score":               "0.9700",
			"match_method":              "path",
			"candidate_count":           "1",
		},
		{
			"apple_track_id":            "3",
			"apple_persistent_id":       "PID3",
			"loved":                     "false",
			"rating":                    "60",
			"artist":                    "Artist3",
			"album":                     "Album3",
			"title":                     "Title3",
			"match_status":              string(report.RemoteMatchStatusLowConfidence),
			"matched_navidrome_song_id": "song3",
			"matched_path":              "/music/three.mp3",
			"match_score":               "0.7600",
			"match_method":              "fuzzy",
			"candidate_count":           "2",
		},
		{
			"apple_track_id":            "4",
			"apple_persistent_id":       "PID4",
			"loved":                     "false",
			"rating":                    "40",
			"artist":                    "Artist4",
			"album":                     "Album4",
			"title":                     "Title4",
			"match_status":              string(report.RemoteMatchStatusMatch),
			"matched_navidrome_song_id": "song4",
			"matched_path":              "/music/four.mp3",
			"match_score":               "0.9500",
			"match_method":              "path",
			"candidate_count":           "1",
		},
	}

	buf := &bytes.Buffer{}
	writer := csv.NewWriter(buf)
	writer.Comma = '\t'
	if err := writer.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	for _, row := range rows {
		record := make([]string, len(header))
		for i, column := range header {
			record[i] = row[column]
		}
		if err := writer.Write(record); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	entries, err := parseRemoteMatchTSV(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("parse TSV: %v", err)
	}

	localIndex := map[int]localTrackMeta{
		1: {Loved: false, Rating: 0},
		2: {Loved: true, Rating: 80},
		3: {Loved: false, Rating: 0},
		4: {Loved: false, Rating: 20},
	}

	filtered, summary := buildRemoteActionableReport(entries, localIndex, false)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 actionable rows, got %d", len(filtered))
	}
	if summary.Total != 2 || summary.LovedOnly != 1 || summary.RatedOnly != 1 || summary.LovedAndRated != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.MatchCount != 2 || summary.LowConfidenceCount != 0 {
		t.Fatalf("unexpected status counts: %+v", summary)
	}

	trackIDs := []int{filtered[0].AppleTrackID, filtered[1].AppleTrackID}
	if !containsTrack(trackIDs, 1) || !containsTrack(trackIDs, 4) {
		t.Fatalf("missing expected track IDs, got %v", trackIDs)
	}

	withLow, summaryLow := buildRemoteActionableReport(entries, localIndex, true)
	if len(withLow) != 3 {
		t.Fatalf("expected 3 actionable rows with low confidence, got %d", len(withLow))
	}
	if summaryLow.LowConfidenceCount != 1 {
		t.Fatalf("expected 1 low-confidence row, got %+v", summaryLow)
	}
}

func containsTrack(ids []int, target int) bool {
	for _, value := range ids {
		if value == target {
			return true
		}
	}
	return false
}
