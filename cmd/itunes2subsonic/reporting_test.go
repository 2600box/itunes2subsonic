package main

import (
	"path/filepath"
	"testing"

	"github.com/logank/itunes2subsonic/internal/report"
)

func TestSyncPlanGoldenCounts(t *testing.T) {
	allowlist := extensionAllowlist(parseExtensions(""))
	libraryPath := filepath.Join("testdata", "library.xml")

	origItunesRoot := *itunesRoot
	origSubsonicRoot := *subsonicRoot
	origMatchMode := *matchMode
	*itunesRoot = "/Music"
	*subsonicRoot = "/Music"
	*matchMode = string(matchModeRealpath)
	defer func() {
		*itunesRoot = origItunesRoot
		*subsonicRoot = origSubsonicRoot
		*matchMode = origMatchMode
	}()

	appleTracks, _, _, stats, err := loadAppleTracks(libraryPath, filterOptions{}, allowlist, false, true)
	if err != nil {
		t.Fatalf("failed to load apple tracks: %s", err)
	}
	if stats.Loved.Local != 2 || stats.Loved.Remote != 1 {
		t.Fatalf("expected loved stats local=2 remote=1, got local=%d remote=%d", stats.Loved.Local, stats.Loved.Remote)
	}

	dumpPath := filepath.Join("testdata", "navidrome_dump.json")
	entries, err := loadNavidromeDump(dumpPath)
	if err != nil {
		t.Fatalf("failed to load navidrome dump: %s", err)
	}
	navidromeSongs := buildNavidromeSongsFromDump(entries, *subsonicRoot, matchModeRealpath, allowlist)

	plan := buildStarUnstarPlanForTest(appleTracks, stats, allowlist, navidromeSongs, nil, matchModeRealpath)
	if plan.Counts.PlannedStar.Total != 2 {
		t.Fatalf("expected 2 planned stars, got %d", plan.Counts.PlannedStar.Total)
	}
	if len(plan.Loved.WontStar) != 1 {
		t.Fatalf("expected 1 loved track not applied, got %d", len(plan.Loved.WontStar))
	}
	if plan.Loved.WontStar[0].NotAppliedReason != reasonRemoteNoLocalMapping {
		t.Fatalf("expected %q, got %q", reasonRemoteNoLocalMapping, plan.Loved.WontStar[0].NotAppliedReason)
	}
}

func TestSyncPlanUnstarSet(t *testing.T) {
	allowlist := extensionAllowlist(parseExtensions(""))
	libraryPath := filepath.Join("testdata", "library.xml")

	origItunesRoot := *itunesRoot
	origSubsonicRoot := *subsonicRoot
	origMatchMode := *matchMode
	*itunesRoot = "/Music"
	*subsonicRoot = "/Music"
	*matchMode = string(matchModeRealpath)
	defer func() {
		*itunesRoot = origItunesRoot
		*subsonicRoot = origSubsonicRoot
		*matchMode = origMatchMode
	}()

	appleTracks, _, _, stats, err := loadAppleTracks(libraryPath, filterOptions{}, allowlist, false, true)
	if err != nil {
		t.Fatalf("failed to load apple tracks: %s", err)
	}
	dumpPath := filepath.Join("testdata", "navidrome_dump.json")
	entries, err := loadNavidromeDump(dumpPath)
	if err != nil {
		t.Fatalf("failed to load navidrome dump: %s", err)
	}
	navidromeSongs := buildNavidromeSongsFromDump(entries, *subsonicRoot, matchModeRealpath, allowlist)
	starred := []navidromeStarredSong{
		{ID: "nav4", Title: "Track Four", Artist: "Artist C", Album: "Album C", Path: "/Music/Artist C/Album C/04 Track Four.mp3"},
	}

	plan := buildStarUnstarPlanForTest(appleTracks, stats, allowlist, navidromeSongs, starred, matchModeRealpath)
	if len(plan.Unstar.WillUnstar) != 1 {
		t.Fatalf("expected 1 unstar candidate, got %d", len(plan.Unstar.WillUnstar))
	}
	if plan.Unstar.WillUnstar[0].Navidrome.SongID != "nav4" {
		t.Fatalf("expected nav4 unstar, got %q", plan.Unstar.WillUnstar[0].Navidrome.SongID)
	}
	if plan.Unstar.WillUnstar[0].Reason != reasonStarredNotLoved {
		t.Fatalf("expected reason %q, got %q", reasonStarredNotLoved, plan.Unstar.WillUnstar[0].Reason)
	}
}

func buildStarUnstarPlanForTest(appleTracks []appleTrackInfo, stats report.LibraryStats, allowlist map[string]struct{}, navidromeSongs []navidromeSong, starredSongs []navidromeStarredSong, selectedMatchMode matchModeValue) report.SyncPlan {
	plan := report.SyncPlan{
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
		Unstar:    report.SyncPlanUnstar{},
		Ratings:   report.SyncPlanRatings{},
		PlayCount: report.SyncPlanPlayCounts{},
		Playlists: report.SyncPlanPlaylists{},
	}

	navidromeByMatch, _ := buildNavidromeIndex(navidromeSongs)
	starredByID := make(map[string]navidromeStarredSong)
	for _, song := range starredSongs {
		starredByID[song.ID] = song
	}
	appleByMatch := make(map[string]appleTrackInfo)
	for _, info := range appleTracks {
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
		reason, matchKey := buildNotAppliedReason(info, allowlist, false)
		appleReport := buildAppleTrackReport(info, matchKey)
		entry := report.LovedPlanEntry{
			Operation: "star",
			Apple:     appleReport,
		}
		if reason != "" {
			entry.Action = "wont_apply"
			entry.NotAppliedReason = reason
			plan.Loved.WontStar = append(plan.Loved.WontStar, entry)
			plan.Counts.LovedNotApplied.Total++
			plan.Counts.LovedNotApplied.ByReason[reason]++
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
			plan.Counts.LovedNotApplied.Total++
			plan.Counts.LovedNotApplied.ByReason[reason]++
			continue
		}
		match := matches[0]
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

	return plan
}
