package main

import (
	"path/filepath"
	"testing"

	"github.com/logank/itunes2subsonic/internal/itunes"
)

func TestBuildUnstarPlanRespectsSyncUnstar(t *testing.T) {
	path := filepath.Join("/music", "Track.mp3")
	matchKey := normalizeMatchPathWithMode(path, "", matchModeRealpath)
	appleByMatch := map[string]appleTrackInfo{
		matchKey: {
			track:    itunes.Track{TrackId: 1, Name: "Track"},
			location: locationParseResult{parsed: path},
			loved:    false,
		},
	}
	starred := []navidromeStarredSong{{ID: "1", Path: path, Title: "Track"}}

	plan := buildUnstarPlan(starred, appleByMatch, matchModeRealpath, false)
	if len(plan.WillUnstar) != 0 {
		t.Fatalf("expected no planned unstar entries when sync_unstar is false")
	}
}
