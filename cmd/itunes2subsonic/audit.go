package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/delucks/go-subsonic"
	"github.com/logank/itunes2subsonic/internal/report"
	pkgreport "github.com/logank/itunes2subsonic/pkg/report"
)

type auditRemoteMatchPaths struct {
	jsonPath string
	tsvPath  string
}

func runAudit(c *subsonic.Client, itunesXML string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool, runDir string, force bool, failOnUnappliedLoved bool, remoteMatchPaths auditRemoteMatchPaths, remoteMatchConfig pkgreport.RemoteMatchConfig, remoteMatchDebug bool) error {
	resolvedRunDir, err := ensureRunDir(runDir, force)
	if err != nil {
		return err
	}
	planPath := filepath.Join(resolvedRunDir, "sync_plan.json")
	reconcilePath := filepath.Join(resolvedRunDir, "reconcile.json")
	dumpPath := filepath.Join(resolvedRunDir, "navidrome_dump.json")
	planTSVBase := filepath.Join(resolvedRunDir, "sync_plan")
	starredBaselinePath := filepath.Join(resolvedRunDir, "navidrome_starred_baseline.tsv")
	auditSummaryPath := filepath.Join(resolvedRunDir, "audit_summary.json")

	plan, stats, navidromeSongs, starredSongs, dstSongs, appleTracks, err := buildSyncPlan(c, itunesXML, filters, allowlist, selectedMatchMode, filterActive, reportOnly)
	if err != nil {
		return err
	}

	if err := writeSyncPlanArtifacts(planPath, plan, selectedMatchMode, planTSVBase); err != nil {
		return err
	}
	if err := writeNavidromeBaselineTSV(starredBaselinePath, starredSongs); err != nil {
		return err
	}
	if reportOnly {
		if *dumpFile == "" {
			return fmt.Errorf("--report_only requires --navidrome_dump for audit mode")
		}
		if err := copyFile(*dumpFile, dumpPath); err != nil {
			return err
		}
	} else if len(dstSongs) > 0 {
		if err := writeNavidromeDump(dumpPath, dstSongs, *subsonicRoot, selectedMatchMode); err != nil {
			return err
		}
	}
	if err := runReportReconcile(itunesXML, planPath, reconcilePath, filters, *allowReconcileMismatch); err != nil {
		return err
	}

	var remoteSummary *report.RemoteMatchSummary
	if remoteMatchPaths.jsonPath != "" || remoteMatchPaths.tsvPath != "" {
		remoteReport, err := runRemoteMatchReport(appleTracks, navidromeSongs, remoteMatchPaths, remoteMatchConfig, remoteMatchDebug)
		if err != nil {
			return err
		}
		remoteSummary = &remoteReport.Summary
	}

	printAuditSummary(stats, plan, remoteSummary)

	auditSummary := buildAuditSummary(stats, plan, remoteSummary)
	if err := report.WriteJSON(auditSummaryPath, auditSummary); err != nil {
		return err
	}

	if failOnUnappliedLoved && plan.Counts.LovedNotApplied.Total > 0 {
		return fmt.Errorf("loved not applied count was %d", plan.Counts.LovedNotApplied.Total)
	}

	return nil
}

func ensureRunDir(runDir string, force bool) (string, error) {
	if runDir == "" {
		runDir = filepath.Join("run", time.Now().UTC().Format("20060102T150405"))
	}
	if info, err := os.Stat(runDir); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("run_dir %q exists and is not a directory", runDir)
		}
		if !force {
			return "", fmt.Errorf("run_dir %q already exists (use --force to overwrite)", runDir)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", err
	}
	return runDir, nil
}

func writeNavidromeBaselineTSV(path string, songs []navidromeStarredSong) error {
	rows := make([][]string, 0, len(songs))
	sort.Slice(songs, func(i, j int) bool {
		if songs[i].Artist != songs[j].Artist {
			return songs[i].Artist < songs[j].Artist
		}
		if songs[i].Album != songs[j].Album {
			return songs[i].Album < songs[j].Album
		}
		if songs[i].Title != songs[j].Title {
			return songs[i].Title < songs[j].Title
		}
		if songs[i].Path != songs[j].Path {
			return songs[i].Path < songs[j].Path
		}
		return songs[i].ID < songs[j].ID
	})
	for _, song := range songs {
		rows = append(rows, []string{
			song.ID,
			song.Artist,
			song.Album,
			song.Title,
			song.Path,
		})
	}
	return report.WriteTSV(path, []string{
		"song_id",
		"artist",
		"album",
		"title",
		"path",
	}, rows)
}

func copyFile(src string, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

func buildAuditSummary(stats report.LibraryStats, plan report.SyncPlan, remoteSummary *report.RemoteMatchSummary) report.AuditSummary {
	predictedFinalStarred := plan.NavidromeSummary.StarredTotal - plan.Counts.PlannedUnstar + plan.Counts.PlannedStar.Total
	invariants := []report.AuditInvariant{
		{
			Name:   "local_loved == local_loved_only + local_loved_and_rated",
			Left:   stats.Loved.Local,
			Right:  stats.LovedOnly.Local + stats.LovedAndRated.Local,
			Passed: stats.Loved.Local == stats.LovedOnly.Local+stats.LovedAndRated.Local,
		},
		{
			Name:   "local_rated == local_rated_only + local_loved_and_rated",
			Left:   stats.Rated.Local,
			Right:  stats.RatedOnly.Local + stats.LovedAndRated.Local,
			Passed: stats.Rated.Local == stats.RatedOnly.Local+stats.LovedAndRated.Local,
		},
		{
			Name:   "predicted_final_starred == navidrome_starred_baseline - planned_unstar + planned_star",
			Left:   predictedFinalStarred,
			Right:  plan.NavidromeSummary.StarredTotal - plan.Counts.PlannedUnstar + plan.Counts.PlannedStar.Total,
			Passed: predictedFinalStarred == plan.NavidromeSummary.StarredTotal-plan.Counts.PlannedUnstar+plan.Counts.PlannedStar.Total,
			Details: map[string]int{
				"navidrome_starred_baseline": plan.NavidromeSummary.StarredTotal,
				"planned_unstar":             plan.Counts.PlannedUnstar,
				"planned_star":               plan.Counts.PlannedStar.Total,
			},
		},
	}

	return report.AuditSummary{
		SchemaVersion:         1,
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
		Version:               buildVersion(),
		Apple:                 stats,
		Navidrome:             plan.NavidromeSummary,
		PlanCounts:            plan.Counts,
		PredictedFinalStarred: predictedFinalStarred,
		Invariants:            invariants,
		RemoteMatchSummary:    remoteSummary,
	}
}

func printAuditSummary(stats report.LibraryStats, plan report.SyncPlan, remoteSummary *report.RemoteMatchSummary) {
	fmt.Fprintln(stdoutWriter, "== Audit Summary ==")
	fmt.Fprintf(stdoutWriter, "Apple Tracks: total=%d local=%d remote=%d\n", stats.Tracks.Total, stats.Tracks.Local, stats.Tracks.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Loved: total=%d local=%d remote=%d\n", stats.Loved.Total, stats.Loved.Local, stats.Loved.Remote)
	fmt.Fprintf(stdoutWriter, "Apple Rated: total=%d local=%d remote=%d\n", stats.Rated.Total, stats.Rated.Local, stats.Rated.Remote)
	fmt.Fprintln(stdoutWriter, "Disaggregation:")
	fmt.Fprintf(stdoutWriter, "  Loved Only: total=%d local=%d remote=%d\n", stats.LovedOnly.Total, stats.LovedOnly.Local, stats.LovedOnly.Remote)
	fmt.Fprintf(stdoutWriter, "  Loved & Rated: total=%d local=%d remote=%d\n", stats.LovedAndRated.Total, stats.LovedAndRated.Local, stats.LovedAndRated.Remote)
	fmt.Fprintf(stdoutWriter, "  Rated Only: total=%d local=%d remote=%d\n", stats.RatedOnly.Total, stats.RatedOnly.Local, stats.RatedOnly.Remote)
	fmt.Fprintf(stdoutWriter, "Navidrome Starred Baseline: total=%d\n", plan.NavidromeSummary.StarredTotal)
	fmt.Fprintf(stdoutWriter, "Planned Star: %d (local=%d remote=%d)\n", plan.Counts.PlannedStar.Total, plan.Counts.PlannedStar.Local, plan.Counts.PlannedStar.Remote)
	fmt.Fprintf(stdoutWriter, "Planned Unstar: %d\n", plan.Counts.PlannedUnstar)
	fmt.Fprintf(stdoutWriter, "Planned Ratings: set=%d unset=%d noop=%d\n", plan.Counts.PlannedRatingsSet, plan.Counts.PlannedRatingsUnset, plan.Counts.PlannedRatingsNoop)
	fmt.Fprintf(stdoutWriter, "Planned Play Count Updates: update=%d noop=%d\n", plan.Counts.PlannedPlaycountUpdates, plan.Counts.PlannedPlaycountNoop)
	fmt.Fprintf(stdoutWriter, "Planned Playlists: create=%d update=%d noop=%d (adds=%d removes=%d)\n", plan.Counts.PlannedPlaylistCreates, plan.Counts.PlannedPlaylistUpdates, plan.Counts.PlannedPlaylistNoop, plan.Counts.PlannedPlaylistTrackAdds, plan.Counts.PlannedPlaylistRemoves)
	fmt.Fprintf(stdoutWriter, "Loved not applied: %d (top reasons: %s)\n", plan.Counts.LovedNotApplied.Total, topReasonSummary(plan.Counts.LovedNotApplied.ByReason, 5))
	printReasonCounts(plan.Counts.LovedNotApplied.ByReason)
	fmt.Fprintf(stdoutWriter, "Rated not applied: %d (top reasons: %s)\n", plan.Counts.RatedNotApplied.Total, topReasonSummary(plan.Counts.RatedNotApplied.ByReason, 5))
	printReasonCounts(plan.Counts.RatedNotApplied.ByReason)

	fmt.Fprintln(stdoutWriter, "Invariants:")
	localLovedRight := stats.LovedOnly.Local + stats.LovedAndRated.Local
	fmt.Fprintf(stdoutWriter, "  local_loved == local_loved_only + local_loved_and_rated: %t (%d == %d)\n", stats.Loved.Local == localLovedRight, stats.Loved.Local, localLovedRight)
	localRatedRight := stats.RatedOnly.Local + stats.LovedAndRated.Local
	fmt.Fprintf(stdoutWriter, "  local_rated == local_rated_only + local_loved_and_rated: %t (%d == %d)\n", stats.Rated.Local == localRatedRight, stats.Rated.Local, localRatedRight)
	predictedFinalStarred := plan.NavidromeSummary.StarredTotal - plan.Counts.PlannedUnstar + plan.Counts.PlannedStar.Total
	fmt.Fprintf(stdoutWriter, "  predicted_final_starred == navidrome_starred_baseline - planned_unstar + planned_star: %d (%d - %d + %d)\n",
		predictedFinalStarred,
		plan.NavidromeSummary.StarredTotal,
		plan.Counts.PlannedUnstar,
		plan.Counts.PlannedStar.Total,
	)

	if remoteSummary != nil {
		fmt.Fprintln(stdoutWriter, "Remote Loved/Rated Matchability:")
		fmt.Fprintf(stdoutWriter, "  Remote Loved: %d\n", remoteSummary.RemoteLovedTotal)
		fmt.Fprintf(stdoutWriter, "  Remote Rated: %d\n", remoteSummary.RemoteRatedTotal)
		fmt.Fprintf(stdoutWriter, "  Remote Loved & Rated: %d\n", remoteSummary.RemoteLovedAndRatedTotal)
		fmt.Fprintf(stdoutWriter, "  Match Status Counts: %s\n", formatReasonCounts(remoteSummary.MatchStatusCounts))
	}
}

func formatReasonCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func runRemoteMatchReport(appleTracks []appleTrackInfo, navidromeSongs []navidromeSong, paths auditRemoteMatchPaths, cfg pkgreport.RemoteMatchConfig, debug bool) (report.RemoteMatchReport, error) {
	remoteTracks := make([]pkgreport.RemoteTrackInput, 0)
	for _, info := range appleTracks {
		if info.trackType != "Remote" {
			continue
		}
		if !info.loved && !info.rated {
			continue
		}
		remoteTracks = append(remoteTracks, pkgreport.RemoteTrackInput{
			AppleTrackID:      info.track.TrackId,
			ApplePersistentID: info.track.PersistentId,
			Loved:             info.loved,
			Rating:            info.track.Rating,
			Artist:            info.track.Artist,
			Album:             info.track.Album,
			Title:             info.track.Name,
		})
	}
	navInputs := make([]pkgreport.NavidromeInput, 0, len(navidromeSongs))
	for _, song := range navidromeSongs {
		navInputs = append(navInputs, pkgreport.NavidromeInput{
			SongID: song.ID,
			Path:   song.Path,
			Artist: song.Artist,
			Album:  song.Album,
			Title:  song.Title,
		})
	}
	result := pkgreport.BuildRemoteMatchReport(buildVersion(), remoteTracks, navInputs, cfg)
	if paths.jsonPath != "" {
		if err := report.WriteJSON(paths.jsonPath, result.Report); err != nil {
			return report.RemoteMatchReport{}, err
		}
	}
	if paths.tsvPath != "" {
		rows := pkgreport.TSVRowsRemoteMatch(result.Report.Entries)
		if err := report.WriteTSV(paths.tsvPath, pkgreport.TSVHeaderRemoteMatch(), rows); err != nil {
			return report.RemoteMatchReport{}, err
		}
	}
	if debug {
		printRemoteMatchDebug(result.Report)
	}
	return result.Report, nil
}

func printRemoteMatchDebug(remoteReport report.RemoteMatchReport) {
	debugCount := 0
	for _, entry := range remoteReport.Entries {
		if entry.MatchStatus == report.RemoteMatchStatusMatch {
			continue
		}
		debugCount++
		if debugCount > 5 {
			break
		}
		payload, _ := json.Marshal(entry.TopCandidates)
		fmt.Fprintf(stdoutWriter, "Remote match debug: apple_track_id=%d status=%s score=%.4f candidates=%s\n", entry.AppleTrackID, entry.MatchStatus, entry.MatchScore, payload)
	}
}
