package main

import (
	"fmt"
	"testing"

	"github.com/logank/itunes2subsonic/internal/itunes"
	"github.com/logank/itunes2subsonic/internal/report"
)

func TestBuildRemoteActionableReportFilters(t *testing.T) {
	baseFileURL := "file:///Volumes/fennec/Music/Media/Music/Unknown%20Artist/Unknown%20Album/"
	basePath := "/Volumes/fennec/Music/Media/Music/Unknown Artist/Unknown Album/"
	trackPathA := basePath + "Andidepressiva-Die Menschmaschiene.mp3"
	trackFileA := baseFileURL + "Andidepressiva-Die%20Menschmaschiene.mp3"
	trackPathB := basePath + "Second Song.mp3"
	trackFileB := baseFileURL + "Second%20Song.mp3"
	trackPathC := basePath + "Third Song.mp3"
	trackFileC := baseFileURL + "Third%20Song.mp3"
	trackPathDup := basePath + "Duplicate Song.mp3"
	trackFileDup := baseFileURL + "Duplicate%20Song.mp3"
	trackPathMissing := basePath + "Missing Song.mp3"

	appleTracks := []appleTrackInfo{
		buildAppleTrackInfo(1, trackFileA, 0, false),
		buildAppleTrackInfo(2, trackFileB, 80, true),
		buildAppleTrackInfo(3, trackFileC, 20, false),
		buildAppleTrackInfo(4, trackFileDup, 0, false),
		buildAppleTrackInfo(5, trackFileDup, 40, true),
	}
	localIndex := buildLocalMetaIndex(appleTracks)

	normalizedPath, ok := normalizeActionableMatchedPath(trackPathA)
	if !ok {
		t.Fatalf("expected normalized path for %q", trackPathA)
	}
	if len(localIndex[normalizedPath]) != 1 {
		t.Fatalf("expected normalized location to match local index, got %d entries", len(localIndex[normalizedPath]))
	}

	entries := []report.RemoteMatchEntry{
		{
			AppleTrackID:      101,
			ApplePersistentID: "PID101",
			Rating:            60,
			MatchStatus:       report.RemoteMatchStatusMatch,
			MatchedSongID:     "song1",
			MatchedPath:       trackPathA,
		},
		{
			AppleTrackID:      102,
			ApplePersistentID: "PID102",
			Loved:             true,
			Rating:            80,
			MatchStatus:       report.RemoteMatchStatusMatch,
			MatchedSongID:     "song2",
			MatchedPath:       trackPathB,
		},
		{
			AppleTrackID:      103,
			ApplePersistentID: "PID103",
			Loved:             true,
			MatchStatus:       report.RemoteMatchStatusMatch,
			MatchedSongID:     "song3",
			MatchedPath:       trackPathC,
		},
		{
			AppleTrackID:      104,
			ApplePersistentID: "PID104",
			Loved:             true,
			MatchStatus:       report.RemoteMatchStatusMatch,
			MatchedSongID:     "song4",
			MatchedPath:       trackPathDup,
		},
		{
			AppleTrackID:      105,
			ApplePersistentID: "PID105",
			Loved:             true,
			MatchStatus:       report.RemoteMatchStatusMatch,
			MatchedSongID:     "song5",
			MatchedPath:       trackPathMissing,
		},
		{
			AppleTrackID:      106,
			ApplePersistentID: "PID106",
			Rating:            40,
			MatchStatus:       report.RemoteMatchStatusLowConfidence,
			MatchedSongID:     "song6",
			MatchedPath:       trackPathA,
		},
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
	if summary.NoLocalPathHit != 1 || summary.AmbiguousLocalPath != 1 {
		t.Fatalf("unexpected path counts: %+v", summary)
	}

	trackIDs := []int{filtered[0].AppleTrackID, filtered[1].AppleTrackID}
	if !containsTrack(trackIDs, 101) || !containsTrack(trackIDs, 103) {
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

func buildAppleTrackInfo(trackID int, location string, rating int, loved bool) appleTrackInfo {
	return appleTrackInfo{
		track: itunes.Track{
			TrackId:      trackID,
			Location:     location,
			Rating:       rating,
			PersistentId: fmt.Sprintf("PID-%d", trackID),
		},
		trackType: "File",
		loved:     loved,
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
