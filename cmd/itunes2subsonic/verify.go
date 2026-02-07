package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/logank/itunes2subsonic/internal/report"
)

const verifyReportSchemaVersion = 1

type verifyThresholds struct {
	MaxStaleMissingStars      int  `json:"max_stale_missing_on_disk_stars"`
	MaxStaleMissingRatings    int  `json:"max_stale_missing_on_disk_ratings"`
	MaxStaleMissingPlaycounts int  `json:"max_stale_missing_on_disk_playcounts"`
	PlaylistInvalidFatal      bool `json:"playlist_invalid_location_fatal"`
}

type verifyReason struct {
	Code      string `json:"code"`
	Domain    string `json:"domain,omitempty"`
	Count     int    `json:"count,omitempty"`
	Threshold int    `json:"threshold,omitempty"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
}

type verifyCounts struct {
	PlannedStarTotal          int                                        `json:"planned_star_total"`
	PlannedUnstarTotal        int                                        `json:"planned_unstar_total"`
	PlannedRatingsSet         int                                        `json:"planned_ratings_set"`
	PlannedRatingsUnset       int                                        `json:"planned_ratings_unset"`
	PlannedPlaycountUpdates   int                                        `json:"planned_playcount_updates"`
	PlannedPlaycountNoop      int                                        `json:"planned_playcount_noop"`
	PlannedPlaylistCreates    int                                        `json:"planned_playlist_creates"`
	PlannedPlaylistUpdates    int                                        `json:"planned_playlist_updates"`
	PlannedPlaylistNoop       int                                        `json:"planned_playlist_noop"`
	PlannedPlaylistTrackAdds  int                                        `json:"planned_playlist_track_adds"`
	PlannedPlaylistRemoves    int                                        `json:"planned_playlist_track_removes"`
	NotAppliedByDomain        map[report.NotAppliedDomain]map[string]int `json:"not_applied_by_domain"`
	NotAppliedAggregateByCode map[string]int                             `json:"not_applied_by_reason"`
}

type verifyReport struct {
	SchemaVersion         int              `json:"schema_version"`
	GeneratedAt           string           `json:"generated_at"`
	Go                    bool             `json:"go"`
	RunDir                string           `json:"run_dir"`
	ConfigPath            string           `json:"config_path,omitempty"`
	PresetName            string           `json:"preset_name,omitempty"`
	AllowUnstar           bool             `json:"allow_unstar"`
	RequireRealPath       bool             `json:"require_real_path"`
	Thresholds            verifyThresholds `json:"thresholds"`
	Counts                verifyCounts     `json:"counts"`
	Reasons               []verifyReason   `json:"reasons,omitempty"`
	Warnings              []verifyReason   `json:"warnings,omitempty"`
	NextCommand           string           `json:"next_command,omitempty"`
	InspectFiles          []string         `json:"inspect_files,omitempty"`
	AuditSummaryPath      string           `json:"audit_summary_path,omitempty"`
	NotAppliedSummaryPath string           `json:"not_applied_summary_path,omitempty"`
}

type verifyConfig struct {
	AllowUnstar bool
	ConfigPath  string
	PresetName  string
	Thresholds  verifyThresholds
}

func buildVerifyReport(summary report.AuditSummary, cfg verifyConfig) verifyReport {
	counts := verifyCounts{
		PlannedStarTotal:          summary.PlanCounts.PlannedStar.Total,
		PlannedUnstarTotal:        summary.PlanCounts.PlannedUnstar,
		PlannedRatingsSet:         summary.PlanCounts.PlannedRatingsSet,
		PlannedRatingsUnset:       summary.PlanCounts.PlannedRatingsUnset,
		PlannedPlaycountUpdates:   summary.PlanCounts.PlannedPlaycountUpdates,
		PlannedPlaycountNoop:      summary.PlanCounts.PlannedPlaycountNoop,
		PlannedPlaylistCreates:    summary.PlanCounts.PlannedPlaylistCreates,
		PlannedPlaylistUpdates:    summary.PlanCounts.PlannedPlaylistUpdates,
		PlannedPlaylistNoop:       summary.PlanCounts.PlannedPlaylistNoop,
		PlannedPlaylistTrackAdds:  summary.PlanCounts.PlannedPlaylistTrackAdds,
		PlannedPlaylistRemoves:    summary.PlanCounts.PlannedPlaylistRemoves,
		NotAppliedByDomain:        make(map[report.NotAppliedDomain]map[string]int),
		NotAppliedAggregateByCode: make(map[string]int),
	}
	for domain, domainSummary := range summary.NotAppliedSummary.ByDomain {
		reasons := make(map[string]int)
		for reason, count := range domainSummary.ByReason {
			reasons[reason] = count
			counts.NotAppliedAggregateByCode[reason] += count
		}
		counts.NotAppliedByDomain[domain] = reasons
	}

	report := verifyReport{
		SchemaVersion:         verifyReportSchemaVersion,
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
		RunDir:                summary.Inputs.RunDir,
		ConfigPath:            cfg.ConfigPath,
		PresetName:            cfg.PresetName,
		AllowUnstar:           cfg.AllowUnstar,
		RequireRealPath:       summary.Inputs.RequireRealPath,
		Thresholds:            cfg.Thresholds,
		Counts:                counts,
		AuditSummaryPath:      summary.Artifacts.AuditSummaryJSON,
		NotAppliedSummaryPath: summary.Artifacts.NotAppliedSummaryJSON,
	}

	var reasons []verifyReason
	var warnings []verifyReason
	addReason := func(reason verifyReason) {
		reasons = append(reasons, reason)
	}
	addWarning := func(reason verifyReason) {
		warnings = append(warnings, reason)
	}

	report.Go = true
	if !summary.Inputs.RequireRealPath {
		report.Go = false
		addReason(verifyReason{
			Code:     "require_real_path_disabled",
			Message:  "require_real_path is disabled; verify is only GO when real paths are enforced",
			Severity: "error",
		})
	}

	if summary.PlanCounts.PlannedUnstar > 0 && !cfg.AllowUnstar {
		report.Go = false
		addReason(verifyReason{
			Code:     "planned_unstar",
			Domain:   "stars",
			Count:    summary.PlanCounts.PlannedUnstar,
			Message:  "planned unstar operations require --allow_unstar",
			Severity: "error",
		})
	}

	invalidStars := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainStars, reasonInvalidLocation)
	if invalidStars > 0 {
		report.Go = false
		addReason(verifyReason{
			Code:     reasonInvalidLocation,
			Domain:   string(report.NotAppliedDomainStars),
			Count:    invalidStars,
			Message:  "invalid_location entries in stars",
			Severity: "error",
		})
	}
	invalidRatings := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainRatings, reasonInvalidLocation)
	if invalidRatings > 0 {
		report.Go = false
		addReason(verifyReason{
			Code:     reasonInvalidLocation,
			Domain:   string(report.NotAppliedDomainRatings),
			Count:    invalidRatings,
			Message:  "invalid_location entries in ratings",
			Severity: "error",
		})
	}

	staleStars := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainStars, reasonStaleMissingOnDisk)
	if staleStars > cfg.Thresholds.MaxStaleMissingStars {
		report.Go = false
		addReason(verifyReason{
			Code:      reasonStaleMissingOnDisk,
			Domain:    string(report.NotAppliedDomainStars),
			Count:     staleStars,
			Threshold: cfg.Thresholds.MaxStaleMissingStars,
			Message:   "stale_missing_on_disk entries in stars exceed threshold",
			Severity:  "error",
		})
	}
	staleRatings := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainRatings, reasonStaleMissingOnDisk)
	if staleRatings > cfg.Thresholds.MaxStaleMissingRatings {
		report.Go = false
		addReason(verifyReason{
			Code:      reasonStaleMissingOnDisk,
			Domain:    string(report.NotAppliedDomainRatings),
			Count:     staleRatings,
			Threshold: cfg.Thresholds.MaxStaleMissingRatings,
			Message:   "stale_missing_on_disk entries in ratings exceed threshold",
			Severity:  "error",
		})
	}
	stalePlays := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainPlaycounts, reasonStaleMissingOnDisk)
	if stalePlays > cfg.Thresholds.MaxStaleMissingPlaycounts {
		report.Go = false
		addReason(verifyReason{
			Code:      reasonStaleMissingOnDisk,
			Domain:    string(report.NotAppliedDomainPlaycounts),
			Count:     stalePlays,
			Threshold: cfg.Thresholds.MaxStaleMissingPlaycounts,
			Message:   "stale_missing_on_disk entries in playcounts exceed threshold",
			Severity:  "error",
		})
	}

	playlistInvalid := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainPlaylists, reasonInvalidLocation)
	if playlistInvalid > 0 {
		message := "invalid_location entries in playlists"
		reason := verifyReason{
			Code:     reasonInvalidLocation,
			Domain:   string(report.NotAppliedDomainPlaylists),
			Count:    playlistInvalid,
			Message:  message,
			Severity: "warning",
		}
		if cfg.Thresholds.PlaylistInvalidFatal {
			reason.Severity = "error"
			report.Go = false
			addReason(reason)
		} else {
			addWarning(reason)
		}
	}

	remoteNoLocal := countNotApplied(summary.NotAppliedSummary, report.NotAppliedDomainPlaylists, reasonRemoteNoLocalMapping)
	if remoteNoLocal > 0 {
		addWarning(verifyReason{
			Code:     reasonRemoteNoLocalMapping,
			Domain:   string(report.NotAppliedDomainPlaylists),
			Count:    remoteNoLocal,
			Message:  "remote Apple Music items without local mappings (playlists)",
			Severity: "warning",
		})
	}

	report.Reasons = reasons
	report.Warnings = warnings

	report.InspectFiles = suggestedInspectFiles(summary, reasons, warnings)
	report.NextCommand = suggestedNextCommand(cfg, report.Go)
	return report
}

func countNotApplied(summary report.NotAppliedSummary, domain report.NotAppliedDomain, reason string) int {
	domainSummary, ok := summary.ByDomain[domain]
	if !ok {
		return 0
	}
	return domainSummary.ByReason[reason]
}

func suggestedInspectFiles(summary report.AuditSummary, reasons []verifyReason, warnings []verifyReason) []string {
	files := make(map[string]struct{})
	for _, reason := range append(reasons, warnings...) {
		switch reason.Domain {
		case string(report.NotAppliedDomainStars):
			files[summary.Artifacts.NotAppliedStarsTSV] = struct{}{}
		case string(report.NotAppliedDomainRatings):
			files[summary.Artifacts.NotAppliedRatingsTSV] = struct{}{}
		case string(report.NotAppliedDomainPlaycounts):
			files[summary.Artifacts.NotAppliedPlaycountsTSV] = struct{}{}
		case string(report.NotAppliedDomainPlaylists):
			files[summary.Artifacts.NotAppliedPlaylistsTSV] = struct{}{}
		}
	}
	if len(files) == 0 {
		return nil
	}
	result := make([]string, 0, len(files))
	for file := range files {
		if file != "" {
			result = append(result, file)
		}
	}
	sort.Strings(result)
	return result
}

func suggestedNextCommand(cfg verifyConfig, goStatus bool) string {
	base := "go run ./cmd/itunes2subsonic"
	parts := []string{base}
	if cfg.ConfigPath != "" {
		parts = append(parts, "--config", cfg.ConfigPath)
	}
	if cfg.PresetName != "" {
		parts = append(parts, "--preset", cfg.PresetName)
	}
	if goStatus {
		parts = append(parts, "--apply")
		if cfg.AllowUnstar {
			parts = append(parts, "--allow_unstar")
		}
		return strings.Join(parts, " ")
	}
	if len(parts) == 1 {
		return "inspect the not_applied_*.tsv files in the run directory"
	}
	parts = append(parts, "--explain_not_applied")
	return strings.Join(parts, " ")
}

func writeVerifyArtifacts(runDir string, verify verifyReport) error {
	path := filepath.Join(runDir, "verify_report.json")
	if err := report.WriteJSON(path, verify); err != nil {
		return err
	}
	summaryPath := filepath.Join(runDir, "summary.txt")
	if err := os.WriteFile(summaryPath, []byte(formatVerifySummary(verify)), 0o600); err != nil {
		return err
	}
	return nil
}

func formatVerifySummary(report verifyReport) string {
	var builder strings.Builder
	status := "GO"
	if !report.Go {
		status = "NO-GO"
	}
	builder.WriteString(fmt.Sprintf("VERIFY RESULT: %s\n", status))
	builder.WriteString(fmt.Sprintf("Run directory: %s\n", report.RunDir))
	if report.ConfigPath != "" {
		builder.WriteString(fmt.Sprintf("Config: %s\n", report.ConfigPath))
	}
	if report.PresetName != "" {
		builder.WriteString(fmt.Sprintf("Preset: %s\n", report.PresetName))
	}
	builder.WriteString("\nPlanned changes:\n")
	builder.WriteString(fmt.Sprintf("  Stars: %d\n", report.Counts.PlannedStarTotal))
	builder.WriteString(fmt.Sprintf("  Unstar: %d\n", report.Counts.PlannedUnstarTotal))
	builder.WriteString(fmt.Sprintf("  Ratings set: %d\n", report.Counts.PlannedRatingsSet))
	builder.WriteString(fmt.Sprintf("  Ratings unset: %d\n", report.Counts.PlannedRatingsUnset))
	builder.WriteString(fmt.Sprintf("  Playcount updates: %d\n", report.Counts.PlannedPlaycountUpdates))
	builder.WriteString(fmt.Sprintf("  Playlists: %d create, %d update, %d noop\n", report.Counts.PlannedPlaylistCreates, report.Counts.PlannedPlaylistUpdates, report.Counts.PlannedPlaylistNoop))

	if len(report.Reasons) > 0 {
		builder.WriteString("\nNO-GO reasons:\n")
		for _, reason := range report.Reasons {
			builder.WriteString(fmt.Sprintf("  - [%s] %s (count=%d)\n", reason.Code, reason.Message, reason.Count))
		}
	}
	if len(report.Warnings) > 0 {
		builder.WriteString("\nWarnings:\n")
		for _, reason := range report.Warnings {
			builder.WriteString(fmt.Sprintf("  - [%s] %s (count=%d)\n", reason.Code, reason.Message, reason.Count))
		}
	}
	if len(report.InspectFiles) > 0 {
		builder.WriteString("\nInspect files:\n")
		for _, file := range report.InspectFiles {
			builder.WriteString(fmt.Sprintf("  - %s\n", file))
		}
	}
	if report.NextCommand != "" {
		builder.WriteString("\nNext command:\n")
		builder.WriteString(fmt.Sprintf("  %s\n", report.NextCommand))
	}
	return builder.String()
}

func printVerifySummary(report verifyReport) {
	fmt.Fprintln(stdoutWriter, formatVerifySummary(report))
}

func normalizeConfigPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func loadVerifyReport(path string) (verifyReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return verifyReport{}, err
	}
	var report verifyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return verifyReport{}, err
	}
	return report, nil
}

func findLatestVerifyReport(runRoot string) (verifyReport, string, error) {
	entries, err := os.ReadDir(runRoot)
	if err != nil {
		return verifyReport{}, "", err
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)
	for i := len(dirs) - 1; i >= 0; i-- {
		path := filepath.Join(runRoot, dirs[i], "verify_report.json")
		if _, err := os.Stat(path); err == nil {
			report, err := loadVerifyReport(path)
			if err != nil {
				return verifyReport{}, "", err
			}
			return report, path, nil
		}
	}
	return verifyReport{}, "", fmt.Errorf("no verify_report.json found under %s", runRoot)
}

func resolveVerifyReportForApply(runDirFlag string, runRoot string) (verifyReport, string, error) {
	if runDirFlag != "" {
		path := filepath.Join(runDirFlag, "verify_report.json")
		if _, err := os.Stat(path); err == nil {
			report, err := loadVerifyReport(path)
			if err != nil {
				return verifyReport{}, "", err
			}
			return report, path, nil
		}
	}
	return findLatestVerifyReport(runRoot)
}
