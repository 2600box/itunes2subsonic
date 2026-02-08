package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/delucks/go-subsonic"
	"github.com/logank/itunes2subsonic/internal/report"
	pkgreport "github.com/logank/itunes2subsonic/pkg/report"
)

type auditRemoteMatchPaths struct {
	jsonPath       string
	tsvPath        string
	actionableTSV  string
	includeLowConf bool
}

type auditResult struct {
	Summary report.AuditSummary
}

type auditOptions struct {
	writeReconcile bool
}

func runAudit(c *subsonic.Client, itunesXML string, filters filterOptions, allowlist map[string]struct{}, selectedMatchMode matchModeValue, filterActive bool, reportOnly bool, runDir string, force bool, failOnUnappliedLoved bool, remoteMatchPaths auditRemoteMatchPaths, remoteMatchConfig pkgreport.RemoteMatchConfig, remoteMatchDebug bool, options auditOptions) (auditResult, error) {
	resolvedRunDir, err := ensureRunDir(runDir, force)
	if err != nil {
		return auditResult{}, err
	}
	planPath := filepath.Join(resolvedRunDir, "sync_plan.json")
	reconcilePath := filepath.Join(resolvedRunDir, "reconcile.json")
	dumpPath := filepath.Join(resolvedRunDir, "navidrome_dump.json")
	planTSVBase := filepath.Join(resolvedRunDir, "sync_plan")
	starredBaselinePath := filepath.Join(resolvedRunDir, "navidrome_starred_baseline.tsv")
	auditSummaryPath := filepath.Join(resolvedRunDir, "audit_summary.json")
	auditSummaryTSVPath := filepath.Join(resolvedRunDir, "audit_summary.tsv")
	notAppliedSummaryPath := filepath.Join(resolvedRunDir, "not_applied_summary.json")
	notAppliedAllTSVPath := filepath.Join(resolvedRunDir, "not_applied_all.tsv")
	notAppliedAllJSONPath := filepath.Join(resolvedRunDir, "not_applied_all.json")
	notAppliedStarsTSVPath := filepath.Join(resolvedRunDir, "not_applied_stars.tsv")
	notAppliedStarsJSONPath := filepath.Join(resolvedRunDir, "not_applied_stars.json")
	notAppliedRatingsTSVPath := filepath.Join(resolvedRunDir, "not_applied_ratings.tsv")
	notAppliedRatingsJSONPath := filepath.Join(resolvedRunDir, "not_applied_ratings.json")
	notAppliedPlaycountsTSVPath := filepath.Join(resolvedRunDir, "not_applied_playcounts.tsv")
	notAppliedPlaycountsJSONPath := filepath.Join(resolvedRunDir, "not_applied_playcounts.json")
	notAppliedPlaylistsTSVPath := filepath.Join(resolvedRunDir, "not_applied_playlists.tsv")
	notAppliedPlaylistsJSONPath := filepath.Join(resolvedRunDir, "not_applied_playlists.json")

	plan, stats, navidromeSongs, starredSongs, dstSongs, appleTracks, err := buildSyncPlan(c, itunesXML, filters, allowlist, selectedMatchMode, filterActive, reportOnly)
	if err != nil {
		return auditResult{}, err
	}

	logPhase("write_reports")
	if err := writeSyncPlanArtifacts(planPath, plan, selectedMatchMode, planTSVBase); err != nil {
		return auditResult{}, err
	}
	if err := writeNavidromeBaselineTSV(starredBaselinePath, starredSongs); err != nil {
		return auditResult{}, err
	}
	if reportOnly {
		if *dumpFile == "" {
			return auditResult{}, fmt.Errorf("--report_only requires --navidrome_dump for audit mode")
		}
		if err := copyFile(*dumpFile, dumpPath); err != nil {
			return auditResult{}, err
		}
	} else if len(dstSongs) > 0 {
		if err := writeNavidromeDump(dumpPath, dstSongs, *subsonicRoot, selectedMatchMode); err != nil {
			return auditResult{}, err
		}
	}
	if options.writeReconcile {
		if err := runReportReconcile(itunesXML, planPath, reconcilePath, filters, *allowReconcileMismatch); err != nil {
			return auditResult{}, err
		}
	} else {
		reconcilePath = ""
	}

	generatedAt := time.Now().UTC()
	notApplied := buildNotAppliedBundle(plan, generatedAt)
	if err := writeNotAppliedArtifacts(notAppliedArtifacts{
		summaryPath:        notAppliedSummaryPath,
		allTSVPath:         notAppliedAllTSVPath,
		allJSONPath:        notAppliedAllJSONPath,
		starsTSVPath:       notAppliedStarsTSVPath,
		starsJSONPath:      notAppliedStarsJSONPath,
		ratingsTSVPath:     notAppliedRatingsTSVPath,
		ratingsJSONPath:    notAppliedRatingsJSONPath,
		playcountsTSVPath:  notAppliedPlaycountsTSVPath,
		playcountsJSONPath: notAppliedPlaycountsJSONPath,
		playlistsTSVPath:   notAppliedPlaylistsTSVPath,
		playlistsJSONPath:  notAppliedPlaylistsJSONPath,
	}, notApplied); err != nil {
		return auditResult{}, err
	}

	var remoteSummary *report.RemoteMatchSummary
	if remoteMatchPaths.jsonPath != "" || remoteMatchPaths.tsvPath != "" || remoteMatchPaths.actionableTSV != "" || remoteMatchDebug {
		remoteReport, err := runRemoteMatchReport(appleTracks, navidromeSongs, remoteMatchPaths, remoteMatchConfig, remoteMatchDebug)
		if err != nil {
			return auditResult{}, err
		}
		remoteSummary = &remoteReport.Summary
	}

	printAuditSummary(stats, plan, notApplied.Summary, remoteSummary)

	auditSummary := buildAuditSummary(auditSummaryInputs{
		stats:            stats,
		plan:             plan,
		remoteSummary:    remoteSummary,
		notApplied:       notApplied.Summary,
		generatedAt:      generatedAt,
		itunesXML:        itunesXML,
		musicRoot:        *musicRoot,
		matchMode:        selectedMatchMode,
		requireRealPath:  *requireRealPath,
		extensions:       parseExtensions(*extensionsFlag),
		reportOnly:       reportOnly,
		runDir:           resolvedRunDir,
		planPath:         planPath,
		reconcilePath:    reconcilePath,
		dumpPath:         dumpPath,
		planTSVBase:      planTSVBase,
		starredTSV:       starredBaselinePath,
		auditSummaryPath: auditSummaryPath,
		auditSummaryTSV:  auditSummaryTSVPath,
		notAppliedPaths: notAppliedPaths{
			summaryJSON:    notAppliedSummaryPath,
			allTSV:         notAppliedAllTSVPath,
			allJSON:        notAppliedAllJSONPath,
			starsTSV:       notAppliedStarsTSVPath,
			starsJSON:      notAppliedStarsJSONPath,
			ratingsTSV:     notAppliedRatingsTSVPath,
			ratingsJSON:    notAppliedRatingsJSONPath,
			playcountsTSV:  notAppliedPlaycountsTSVPath,
			playcountsJSON: notAppliedPlaycountsJSONPath,
			playlistsTSV:   notAppliedPlaylistsTSVPath,
			playlistsJSON:  notAppliedPlaylistsJSONPath,
		},
		remoteMatchPaths: remoteMatchPaths,
	})
	if err := report.WriteJSON(auditSummaryPath, auditSummary); err != nil {
		return auditResult{}, err
	}
	if err := writeAuditSummaryTSV(auditSummaryTSVPath, auditSummary); err != nil {
		return auditResult{}, err
	}

	if failOnUnappliedLoved && plan.Counts.LovedNotApplied.Total > 0 {
		return auditResult{}, fmt.Errorf("loved not applied count was %d", plan.Counts.LovedNotApplied.Total)
	}

	return auditResult{Summary: auditSummary}, nil
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

type auditSummaryInputs struct {
	stats            report.LibraryStats
	plan             report.SyncPlan
	remoteSummary    *report.RemoteMatchSummary
	notApplied       report.NotAppliedSummary
	generatedAt      time.Time
	itunesXML        string
	musicRoot        string
	matchMode        matchModeValue
	requireRealPath  bool
	extensions       []string
	reportOnly       bool
	runDir           string
	planPath         string
	reconcilePath    string
	dumpPath         string
	planTSVBase      string
	starredTSV       string
	auditSummaryPath string
	auditSummaryTSV  string
	notAppliedPaths  notAppliedPaths
	remoteMatchPaths auditRemoteMatchPaths
}

type notAppliedPaths struct {
	summaryJSON    string
	allTSV         string
	allJSON        string
	starsTSV       string
	starsJSON      string
	ratingsTSV     string
	ratingsJSON    string
	playcountsTSV  string
	playcountsJSON string
	playlistsTSV   string
	playlistsJSON  string
}

func buildAuditSummary(inputs auditSummaryInputs) report.AuditSummary {
	predictedFinalStarred := inputs.plan.NavidromeSummary.StarredTotal - inputs.plan.Counts.PlannedUnstar + inputs.plan.Counts.PlannedStar.Total
	invariants := []report.AuditInvariant{
		{
			Name:   "local_loved == local_loved_only + local_loved_and_rated",
			Left:   inputs.stats.Loved.Local,
			Right:  inputs.stats.LovedOnly.Local + inputs.stats.LovedAndRated.Local,
			Passed: inputs.stats.Loved.Local == inputs.stats.LovedOnly.Local+inputs.stats.LovedAndRated.Local,
		},
		{
			Name:   "local_rated == local_rated_only + local_loved_and_rated",
			Left:   inputs.stats.Rated.Local,
			Right:  inputs.stats.RatedOnly.Local + inputs.stats.LovedAndRated.Local,
			Passed: inputs.stats.Rated.Local == inputs.stats.RatedOnly.Local+inputs.stats.LovedAndRated.Local,
		},
		{
			Name:   "predicted_final_starred == navidrome_starred_baseline - planned_unstar + planned_star",
			Left:   predictedFinalStarred,
			Right:  inputs.plan.NavidromeSummary.StarredTotal - inputs.plan.Counts.PlannedUnstar + inputs.plan.Counts.PlannedStar.Total,
			Passed: predictedFinalStarred == inputs.plan.NavidromeSummary.StarredTotal-inputs.plan.Counts.PlannedUnstar+inputs.plan.Counts.PlannedStar.Total,
			Details: map[string]int{
				"navidrome_starred_baseline": inputs.plan.NavidromeSummary.StarredTotal,
				"planned_unstar":             inputs.plan.Counts.PlannedUnstar,
				"planned_star":               inputs.plan.Counts.PlannedStar.Total,
			},
		},
	}

	itunesBase := ""
	if inputs.itunesXML != "" {
		itunesBase = filepath.Base(inputs.itunesXML)
	}
	return report.AuditSummary{
		SchemaVersion: 2,
		GeneratedAt:   inputs.generatedAt.UTC().Format(time.RFC3339),
		Version:       buildVersion(),
		GitCommit:     buildGitCommit(),
		Inputs: report.AuditInputs{
			ItunesXML:       itunesBase,
			MusicRoot:       inputs.musicRoot,
			SubsonicURL:     sanitizeSubsonicURL(*subsonicUrl),
			MatchMode:       string(inputs.matchMode),
			RequireRealPath: inputs.requireRealPath,
			Extensions:      append([]string{}, inputs.extensions...),
			RunDir:          inputs.runDir,
			ReportOnly:      inputs.reportOnly,
		},
		Artifacts: report.AuditArtifacts{
			NavidromeDump:            inputs.dumpPath,
			SyncPlan:                 inputs.planPath,
			Reconcile:                inputs.reconcilePath,
			SyncPlanTSVBase:          inputs.planTSVBase,
			NavidromeStarredTSV:      inputs.starredTSV,
			AuditSummaryJSON:         inputs.auditSummaryPath,
			AuditSummaryTSV:          inputs.auditSummaryTSV,
			NotAppliedSummaryJSON:    inputs.notAppliedPaths.summaryJSON,
			NotAppliedAllTSV:         inputs.notAppliedPaths.allTSV,
			NotAppliedAllJSON:        inputs.notAppliedPaths.allJSON,
			NotAppliedStarsTSV:       inputs.notAppliedPaths.starsTSV,
			NotAppliedStarsJSON:      inputs.notAppliedPaths.starsJSON,
			NotAppliedRatingsTSV:     inputs.notAppliedPaths.ratingsTSV,
			NotAppliedRatingsJSON:    inputs.notAppliedPaths.ratingsJSON,
			NotAppliedPlaycountsTSV:  inputs.notAppliedPaths.playcountsTSV,
			NotAppliedPlaycountsJSON: inputs.notAppliedPaths.playcountsJSON,
			NotAppliedPlaylistsTSV:   inputs.notAppliedPaths.playlistsTSV,
			NotAppliedPlaylistsJSON:  inputs.notAppliedPaths.playlistsJSON,
			RemoteMatchJSON:          inputs.remoteMatchPaths.jsonPath,
			RemoteMatchTSV:           inputs.remoteMatchPaths.tsvPath,
		},
		Apple:                 inputs.stats,
		Navidrome:             inputs.plan.NavidromeSummary,
		PlanCounts:            inputs.plan.Counts,
		NotAppliedSummary:     inputs.notApplied,
		PredictedFinalStarred: predictedFinalStarred,
		Invariants:            invariants,
		RemoteMatchSummary:    inputs.remoteSummary,
	}
}

func printAuditSummary(stats report.LibraryStats, plan report.SyncPlan, notApplied report.NotAppliedSummary, remoteSummary *report.RemoteMatchSummary) {
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
	fmt.Fprintln(stdoutWriter, "Not Applied (by domain):")
	printNotAppliedDomainSummary("Stars", notApplied.ByDomain[report.NotAppliedDomainStars])
	printNotAppliedDomainSummary("Ratings", notApplied.ByDomain[report.NotAppliedDomainRatings])
	printNotAppliedDomainSummary("Playcounts", notApplied.ByDomain[report.NotAppliedDomainPlaycounts])
	printNotAppliedDomainSummary("Playlists", notApplied.ByDomain[report.NotAppliedDomainPlaylists])

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

type notAppliedArtifacts struct {
	summaryPath        string
	allTSVPath         string
	allJSONPath        string
	starsTSVPath       string
	starsJSONPath      string
	ratingsTSVPath     string
	ratingsJSONPath    string
	playcountsTSVPath  string
	playcountsJSONPath string
	playlistsTSVPath   string
	playlistsJSONPath  string
}

func writeNotAppliedArtifacts(paths notAppliedArtifacts, bundle notAppliedBundle) error {
	if err := report.WriteJSON(paths.summaryPath, bundle.Summary); err != nil {
		return err
	}
	if err := writeNotAppliedDomain(paths.allTSVPath, paths.allJSONPath, report.NotAppliedDomainAll, bundle.RowsAll); err != nil {
		return err
	}
	if err := writeNotAppliedDomain(paths.starsTSVPath, paths.starsJSONPath, report.NotAppliedDomainStars, bundle.RowsByDomain[report.NotAppliedDomainStars]); err != nil {
		return err
	}
	if err := writeNotAppliedDomain(paths.ratingsTSVPath, paths.ratingsJSONPath, report.NotAppliedDomainRatings, bundle.RowsByDomain[report.NotAppliedDomainRatings]); err != nil {
		return err
	}
	if err := writeNotAppliedDomain(paths.playcountsTSVPath, paths.playcountsJSONPath, report.NotAppliedDomainPlaycounts, bundle.RowsByDomain[report.NotAppliedDomainPlaycounts]); err != nil {
		return err
	}
	if err := writeNotAppliedDomain(paths.playlistsTSVPath, paths.playlistsJSONPath, report.NotAppliedDomainPlaylists, bundle.RowsByDomain[report.NotAppliedDomainPlaylists]); err != nil {
		return err
	}
	return nil
}

func writeNotAppliedDomain(tsvPath string, jsonPath string, domain report.NotAppliedDomain, rows []report.NotAppliedRow) error {
	if err := report.WriteTSV(tsvPath, notAppliedTSVHeader(), buildNotAppliedTSVRows(rows)); err != nil {
		return err
	}
	truncated := false
	outputRows := rows
	if len(rows) > notAppliedRowLimit {
		outputRows = rows[:notAppliedRowLimit]
		truncated = true
	}
	domainReport := report.NotAppliedDomainReport{
		SchemaVersion: notAppliedSummarySchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Domain:        domain,
		TotalRows:     len(rows),
		Truncated:     truncated,
		Rows:          outputRows,
	}
	return report.WriteJSON(jsonPath, domainReport)
}

func writeAuditSummaryTSV(path string, summary report.AuditSummary) error {
	rows := make([][]string, 0)
	appendRow := func(key string, value string) {
		rows = append(rows, []string{key, value})
	}
	appendRow("schema_version", strconv.Itoa(summary.SchemaVersion))
	appendRow("generated_at", summary.GeneratedAt)
	appendRow("git_commit", summary.GitCommit)
	appendRow("itunes_xml", summary.Inputs.ItunesXML)
	appendRow("music_root", summary.Inputs.MusicRoot)
	appendRow("subsonic_url", summary.Inputs.SubsonicURL)
	appendRow("match_mode", summary.Inputs.MatchMode)
	appendRow("require_real_path", strconv.FormatBool(summary.Inputs.RequireRealPath))
	appendRow("extensions", strings.Join(summary.Inputs.Extensions, ","))
	appendRow("run_dir", summary.Inputs.RunDir)
	appendRow("report_only", strconv.FormatBool(summary.Inputs.ReportOnly))
	appendRow("apple_tracks_total", strconv.Itoa(summary.Apple.Tracks.Total))
	appendRow("apple_tracks_local", strconv.Itoa(summary.Apple.Tracks.Local))
	appendRow("apple_tracks_remote", strconv.Itoa(summary.Apple.Tracks.Remote))
	appendRow("apple_loved_total", strconv.Itoa(summary.Apple.Loved.Total))
	appendRow("apple_rated_total", strconv.Itoa(summary.Apple.Rated.Total))
	appendRow("navidrome_tracks_total", strconv.Itoa(summary.Navidrome.TracksTotal))
	appendRow("navidrome_starred_total", strconv.Itoa(summary.Navidrome.StarredTotal))
	appendRow("navidrome_rated_total", strconv.Itoa(summary.Navidrome.RatedTotal))
	appendRow("planned_star_total", strconv.Itoa(summary.PlanCounts.PlannedStar.Total))
	appendRow("planned_unstar_total", strconv.Itoa(summary.PlanCounts.PlannedUnstar))
	appendRow("planned_ratings_set", strconv.Itoa(summary.PlanCounts.PlannedRatingsSet))
	appendRow("planned_ratings_unset", strconv.Itoa(summary.PlanCounts.PlannedRatingsUnset))
	appendRow("planned_playcount_updates", strconv.Itoa(summary.PlanCounts.PlannedPlaycountUpdates))
	appendRow("planned_playcount_noop", strconv.Itoa(summary.PlanCounts.PlannedPlaycountNoop))
	appendRow("planned_playlist_creates", strconv.Itoa(summary.PlanCounts.PlannedPlaylistCreates))
	appendRow("planned_playlist_updates", strconv.Itoa(summary.PlanCounts.PlannedPlaylistUpdates))
	appendRow("planned_playlist_noop", strconv.Itoa(summary.PlanCounts.PlannedPlaylistNoop))
	appendRow("not_applied_total", strconv.Itoa(summary.NotAppliedSummary.TotalRows))
	appendNotAppliedTSVRows(summary.NotAppliedSummary, appendRow)
	return report.WriteTSV(path, []string{"metric", "value"}, rows)
}

func appendNotAppliedTSVRows(summary report.NotAppliedSummary, appendRow func(string, string)) {
	domains := make([]report.NotAppliedDomain, 0, len(summary.ByDomain))
	for domain := range summary.ByDomain {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool {
		return domains[i] < domains[j]
	})
	for _, domain := range domains {
		domainSummary := summary.ByDomain[domain]
		appendRow("not_applied."+string(domain)+".total", strconv.Itoa(domainSummary.Total))
		reasons := make([]string, 0, len(domainSummary.ByReason))
		for reason := range domainSummary.ByReason {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			appendRow("not_applied."+string(domain)+".by_reason."+reason, strconv.Itoa(domainSummary.ByReason[reason]))
		}
	}
}

func printNotAppliedDomainSummary(label string, domainSummary report.NotAppliedDomainSummary) {
	if domainSummary.Total == 0 {
		fmt.Fprintf(stdoutWriter, "  %s: total=0\n", label)
		return
	}
	fmt.Fprintf(stdoutWriter, "  %s: total=%d (top reasons: %s)\n", label, domainSummary.Total, topReasonSummary(domainSummary.ByReason, 5))
	printReasonCounts(domainSummary.ByReason)
}

func sanitizeSubsonicURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
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
	if paths.jsonPath == "" && paths.tsvPath == "" && paths.actionableTSV == "" && !debug {
		return report.RemoteMatchReport{}, nil
	}
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
	if paths.actionableTSV != "" {
		localIndex := buildLocalMetaIndex(appleTracks)
		rows, summary := buildRemoteActionableReport(result.Report.Entries, localIndex, paths.includeLowConf)
		if err := report.WriteTSV(paths.actionableTSV, remoteActionableTSVHeader(), remoteActionableTSVRows(rows)); err != nil {
			return report.RemoteMatchReport{}, err
		}
		log.Printf("Remote actionable: total=%d loved_only=%d rated_only=%d loved_and_rated=%d; by_status MATCH=%d LOW_CONFIDENCE=%d",
			summary.Total,
			summary.LovedOnly,
			summary.RatedOnly,
			summary.LovedAndRated,
			summary.MatchCount,
			summary.LowConfidenceCount,
		)
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
