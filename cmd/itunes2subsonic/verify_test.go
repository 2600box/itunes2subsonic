package main

import (
	"testing"

	"github.com/logank/itunes2subsonic/internal/report"
)

func TestVerifyRequiresRealPath(t *testing.T) {
	summary := baseVerifySummary()
	summary.Inputs.RequireRealPath = false

	report := buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{},
	})
	if report.Go {
		t.Fatalf("expected NO-GO when require_real_path is disabled")
	}
}

func TestVerifyPlannedUnstarRequiresAllow(t *testing.T) {
	summary := baseVerifySummary()
	summary.PlanCounts.PlannedUnstar = 2

	report := buildVerifyReport(summary, verifyConfig{
		AllowUnstar: false,
		Thresholds:  verifyThresholds{},
	})
	if report.Go {
		t.Fatalf("expected NO-GO when unstar planned without allow_unstar")
	}

	report = buildVerifyReport(summary, verifyConfig{
		AllowUnstar: true,
		Thresholds:  verifyThresholds{},
	})
	if !report.Go {
		t.Fatalf("expected GO when allow_unstar is set")
	}
}

func TestVerifyInvalidLocationIsFatalForStarsAndRatings(t *testing.T) {
	summary := baseVerifySummary()
	summary.NotAppliedSummary.ByDomain[report.NotAppliedDomainStars] = report.NotAppliedDomainSummary{
		Total:    1,
		ByReason: map[string]int{reasonInvalidLocation: 1},
	}

	verifyReport := buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{},
	})
	if verifyReport.Go {
		t.Fatalf("expected NO-GO for invalid_location in stars")
	}
}

func TestVerifyStaleMissingThresholds(t *testing.T) {
	summary := baseVerifySummary()
	summary.NotAppliedSummary.ByDomain[report.NotAppliedDomainRatings] = report.NotAppliedDomainSummary{
		Total:    2,
		ByReason: map[string]int{reasonStaleMissingOnDisk: 2},
	}

	verifyReport := buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{MaxStaleMissingRatings: 1},
	})
	if verifyReport.Go {
		t.Fatalf("expected NO-GO when stale_missing_on_disk exceeds threshold")
	}

	verifyReport = buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{MaxStaleMissingRatings: 2},
	})
	if !verifyReport.Go {
		t.Fatalf("expected GO when stale_missing_on_disk meets threshold")
	}
}

func TestVerifyPlaylistInvalidLocationDefaultWarning(t *testing.T) {
	summary := baseVerifySummary()
	summary.NotAppliedSummary.ByDomain[report.NotAppliedDomainPlaylists] = report.NotAppliedDomainSummary{
		Total:    3,
		ByReason: map[string]int{reasonInvalidLocation: 3},
	}

	verifyReport := buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{PlaylistInvalidFatal: false},
	})
	if !verifyReport.Go {
		t.Fatalf("expected GO when playlist invalid_location is non-fatal")
	}
	if len(verifyReport.Warnings) == 0 {
		t.Fatalf("expected warning for playlist invalid_location")
	}

	verifyReport = buildVerifyReport(summary, verifyConfig{
		Thresholds: verifyThresholds{PlaylistInvalidFatal: true},
	})
	if verifyReport.Go {
		t.Fatalf("expected NO-GO when playlist invalid_location is fatal")
	}
}

func baseVerifySummary() report.AuditSummary {
	return report.AuditSummary{
		Inputs: report.AuditInputs{
			RequireRealPath: true,
			RunDir:          "run/20240101T120000",
		},
		Artifacts: report.AuditArtifacts{
			NotAppliedStarsTSV:     "run/20240101T120000/not_applied_stars.tsv",
			NotAppliedRatingsTSV:   "run/20240101T120000/not_applied_ratings.tsv",
			NotAppliedPlaylistsTSV: "run/20240101T120000/not_applied_playlists.tsv",
		},
		PlanCounts: report.SyncPlanCounts{
			PlannedStar: report.PlanCountsBySource{},
		},
		NotAppliedSummary: report.NotAppliedSummary{
			ByDomain: map[report.NotAppliedDomain]report.NotAppliedDomainSummary{
				report.NotAppliedDomainStars:      {ByReason: map[string]int{}},
				report.NotAppliedDomainRatings:    {ByReason: map[string]int{}},
				report.NotAppliedDomainPlaycounts: {ByReason: map[string]int{}},
				report.NotAppliedDomainPlaylists:  {ByReason: map[string]int{}},
			},
		},
	}
}
