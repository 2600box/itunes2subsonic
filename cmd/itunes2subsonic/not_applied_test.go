package main

import (
	"testing"
	"time"

	"github.com/logank/itunes2subsonic/internal/report"
)

func TestNotAppliedSummaryByDomainCounts(t *testing.T) {
	playcountEntries := make([]report.PlayCountPlanEntry, 0, 45)
	for i := 0; i < 45; i++ {
		playcountEntries = append(playcountEntries, report.PlayCountPlanEntry{
			Action: "wont_apply",
			Reason: reasonExcludedExtension,
			Apple: report.AppleTrack{
				TrackID: i + 1,
				Name:    "Track",
			},
		})
	}
	ratingEntries := make([]report.RatingPlanEntry, 0, 4)
	for i := 0; i < 4; i++ {
		ratingEntries = append(ratingEntries, report.RatingPlanEntry{
			Action:           "wont_apply",
			NotAppliedReason: reasonExcludedExtension,
			Apple: report.AppleTrack{
				TrackID: 100 + i,
				Name:    "Rated",
			},
		})
	}

	plan := report.SyncPlan{
		PlayCount: report.SyncPlanPlayCounts{
			WontUpdate: playcountEntries,
		},
		Ratings: report.SyncPlanRatings{
			WontSet: ratingEntries,
		},
	}

	bundle := buildNotAppliedBundle(plan, time.Now())
	playcounts := bundle.Summary.ByDomain[report.NotAppliedDomainPlaycounts]
	if playcounts.ByReason[reasonExcludedExtension] != 45 {
		t.Fatalf("playcounts excluded_extension=%d want=45", playcounts.ByReason[reasonExcludedExtension])
	}
	ratings := bundle.Summary.ByDomain[report.NotAppliedDomainRatings]
	if ratings.ByReason[reasonExcludedExtension] != 4 {
		t.Fatalf("ratings excluded_extension=%d want=4", ratings.ByReason[reasonExcludedExtension])
	}
	if bundle.Summary.AggregateByReason[reasonExcludedExtension] != 49 {
		t.Fatalf("aggregate excluded_extension=%d want=49", bundle.Summary.AggregateByReason[reasonExcludedExtension])
	}
}

func TestNotAppliedInvalidLocationIncludesRawPath(t *testing.T) {
	plan := report.SyncPlan{
		Loved: report.SyncPlanLoved{
			WontStar: []report.LovedPlanEntry{
				{
					Action:           "wont_apply",
					NotAppliedReason: reasonInvalidLocation,
					Apple: report.AppleTrack{
						TrackID:   42,
						Name:      "Broken",
						Artist:    "Artist",
						PathRaw:   "file://localhost/Volumes/Music/Broken.mp4",
						PathClean: "",
					},
				},
			},
		},
	}
	bundle := buildNotAppliedBundle(plan, time.Now())
	rows := bundle.RowsByDomain[report.NotAppliedDomainStars]
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ApplePathRaw == "" || rows[0].ApplePathRaw != "file://localhost/Volumes/Music/Broken.mp4" {
		t.Fatalf("expected raw path to be preserved, got %q", rows[0].ApplePathRaw)
	}
}

func TestNotAppliedPlaylistMissingTracksCaptured(t *testing.T) {
	plan := report.SyncPlan{
		Playlists: report.SyncPlanPlaylists{
			Entries: []report.PlaylistPlanEntry{
				{
					Name: "Favorites",
					MissingTracks: []report.PlaylistTrackRef{
						{
							AppleTrackID: 55,
							Title:        "Missing Track",
							Artist:       "Artist",
							Album:        "Album",
							Path:         "/music/missing.mp3",
							PathRaw:      "file://localhost/music/missing.mp3",
							Reason:       reasonInvalidLocation,
						},
					},
				},
			},
		},
	}
	bundle := buildNotAppliedBundle(plan, time.Now())
	rows := bundle.RowsByDomain[report.NotAppliedDomainPlaylists]
	if len(rows) != 1 {
		t.Fatalf("expected 1 playlist row, got %d", len(rows))
	}
	if rows[0].PlaylistName != "Favorites" {
		t.Fatalf("expected playlist name to be captured, got %q", rows[0].PlaylistName)
	}
	if rows[0].ApplePathRaw == "" {
		t.Fatalf("expected playlist raw path to be captured")
	}
}
