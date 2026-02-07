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

func TestRunReportReconcileSkipsWhenPathEmpty(t *testing.T) {
	err := runReportReconcile("missing.xml", "missing.json", "", filterOptions{}, false)
	if err != nil {
		t.Fatalf("expected no error when reconcile path is empty, got %s", err)
	}
}

func TestRunReportSyncPlanSkipsWhenPathEmpty(t *testing.T) {
	plan, stats, navidromeSongs, appleTracks, err := runReportSyncPlanWithData(nil, "missing.xml", "", filterOptions{}, nil, matchModeRealpath, false, false, "")
	if err != nil {
		t.Fatalf("expected no error when sync plan path is empty, got %s", err)
	}
	if navidromeSongs != nil || appleTracks != nil {
		t.Fatalf("expected no data when sync plan is skipped")
	}
	if plan.SchemaVersion != 0 || stats.Tracks.Total != 0 {
		t.Fatalf("expected empty plan and stats when sync plan is skipped")
	}
}
