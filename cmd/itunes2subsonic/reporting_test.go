package main

import (
	"reflect"
	"testing"

	"github.com/logank/itunes2subsonic/internal/report"
)

func TestBuildSyncPlanErrorReturnsZeroValues(t *testing.T) {
	originalDumpFile := *dumpFile
	t.Cleanup(func() {
		*dumpFile = originalDumpFile
	})
	*dumpFile = ""

	plan, stats, navidromeSongs, starredSongs, dstSongs, appleTracks, err := buildSyncPlan(nil, "", filterOptions{}, nil, matchModeRealpath, false, true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !reflect.DeepEqual(plan, report.SyncPlan{}) {
		t.Fatalf("expected empty plan, got %#v", plan)
	}
	if !reflect.DeepEqual(stats, report.LibraryStats{}) {
		t.Fatalf("expected empty stats, got %#v", stats)
	}
	if navidromeSongs != nil || starredSongs != nil || dstSongs != nil || appleTracks != nil {
		t.Fatalf("expected nil slices, got navidrome=%v starred=%v dst=%v apple=%v", navidromeSongs, starredSongs, dstSongs, appleTracks)
	}
}

func TestRunReportSyncPlanWithDataErrorReturnsZeroValues(t *testing.T) {
	plan, stats, navidromeSongs, appleTracks, err := runReportSyncPlanWithData(nil, "", "", filterOptions{}, nil, matchModeRealpath, false, false, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !reflect.DeepEqual(plan, report.SyncPlan{}) {
		t.Fatalf("expected empty plan, got %#v", plan)
	}
	if !reflect.DeepEqual(stats, report.LibraryStats{}) {
		t.Fatalf("expected empty stats, got %#v", stats)
	}
	if navidromeSongs != nil || appleTracks != nil {
		t.Fatalf("expected nil slices, got navidrome=%v apple=%v", navidromeSongs, appleTracks)
	}
}
