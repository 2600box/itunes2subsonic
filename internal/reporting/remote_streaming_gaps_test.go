package reporting

import (
	"testing"

	"github.com/logank/itunes2subsonic/internal/report"
)

func TestRemoteStreamingAlignedExcludedFromMissingMetadata(t *testing.T) {
	apple := []RemoteStreamingAppleTrack{
		{
			TrackID:         1,
			Title:           "Letters From Rome",
			Artist:          "Fennec",
			Album:           "Travel Notes",
			Rating:          100,
			Loved:           true,
			DurationSeconds: 210,
		},
	}
	navidrome := []RemoteStreamingNavidromeTrack{
		{
			SongID:          "song-1",
			Title:           "Letters From Rome",
			Artist:          "Fennec",
			Album:           "Travel Notes",
			Rating:          5,
			Starred:         true,
			DurationSeconds: 210,
		},
	}

	result := BuildRemoteStreamingGapReport("test", apple, navidrome)
	if result.Summary.AlignedCount != 1 {
		t.Fatalf("expected aligned count 1, got %d", result.Summary.AlignedCount)
	}
	if result.Summary.PresentButMissingMetadataCount != 0 {
		t.Fatalf("expected present but missing metadata count 0, got %d", result.Summary.PresentButMissingMetadataCount)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.MatchStatus != report.RemoteStreamingGapStatusMatch {
		t.Fatalf("expected match status MATCH, got %s", entry.MatchStatus)
	}
	if len(entry.GapFlags) != 0 {
		t.Fatalf("expected no gap flags, got %v", entry.GapFlags)
	}
}

func TestRemoteStreamingRatingMismatchFlags(t *testing.T) {
	apple := []RemoteStreamingAppleTrack{
		{
			TrackID:         2,
			Title:           "Midnight Train",
			Artist:          "Signal",
			Album:           "Signals",
			Rating:          80,
			Loved:           false,
			DurationSeconds: 180,
		},
	}
	navidrome := []RemoteStreamingNavidromeTrack{
		{
			SongID:          "song-2",
			Title:           "Midnight Train",
			Artist:          "Signal",
			Album:           "Signals",
			Rating:          0,
			Starred:         false,
			DurationSeconds: 180,
		},
	}

	result := BuildRemoteStreamingGapReport("test", apple, navidrome)
	if result.Summary.PresentButMissingMetadataCount != 1 {
		t.Fatalf("expected present but missing metadata count 1, got %d", result.Summary.PresentButMissingMetadataCount)
	}
	entry := result.Entries[0]
	if entry.MatchStatus != report.RemoteStreamingGapStatusMatch {
		t.Fatalf("expected match status MATCH, got %s", entry.MatchStatus)
	}
	if !containsGapFlag(entry.GapFlags, "rating_missing") {
		t.Fatalf("expected rating_missing flag, got %v", entry.GapFlags)
	}
}

func TestRemoteStreamingAmbiguousCandidates(t *testing.T) {
	apple := []RemoteStreamingAppleTrack{
		{
			TrackID:         3,
			Title:           "Shared Title",
			Artist:          "Twin Acts",
			Album:           "",
			Rating:          100,
			Loved:           true,
			DurationSeconds: 200,
		},
	}
	navidrome := []RemoteStreamingNavidromeTrack{
		{
			SongID:          "song-3",
			Title:           "Shared Title",
			Artist:          "Twin Acts",
			Album:           "Album A",
			Rating:          5,
			Starred:         true,
			DurationSeconds: 200,
		},
		{
			SongID:          "song-4",
			Title:           "Shared Title",
			Artist:          "Twin Acts",
			Album:           "Album B",
			Rating:          5,
			Starred:         true,
			DurationSeconds: 200,
		},
	}

	result := BuildRemoteStreamingGapReport("test", apple, navidrome)
	if result.Summary.AmbiguousCount != 1 {
		t.Fatalf("expected ambiguous count 1, got %d", result.Summary.AmbiguousCount)
	}
	entry := result.Entries[0]
	if entry.MatchStatus != report.RemoteStreamingGapStatusAmbiguous {
		t.Fatalf("expected match status AMBIGUOUS, got %s", entry.MatchStatus)
	}
}

func containsGapFlag(flags []string, target string) bool {
	for _, flag := range flags {
		if flag == target {
			return true
		}
	}
	return false
}
