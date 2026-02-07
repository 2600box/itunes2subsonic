package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/logank/itunes2subsonic/internal/report"
)

type explainGroupKey struct {
	Domain report.NotAppliedDomain
	Reason string
}

func explainNotAppliedFromRunDir(runDir string, topN int) error {
	auditSummaryPath := filepath.Join(runDir, "audit_summary.json")
	data, err := os.ReadFile(auditSummaryPath)
	if err != nil {
		return fmt.Errorf("failed to read audit summary %q: %w", auditSummaryPath, err)
	}
	var summary report.AuditSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return fmt.Errorf("failed to parse audit summary: %w", err)
	}
	if summary.Artifacts.NotAppliedAllJSON == "" {
		return fmt.Errorf("audit summary missing not_applied_all.json path")
	}
	reportData, err := os.ReadFile(summary.Artifacts.NotAppliedAllJSON)
	if err != nil {
		return fmt.Errorf("failed to read not_applied_all.json: %w", err)
	}
	var domainReport report.NotAppliedDomainReport
	if err := json.Unmarshal(reportData, &domainReport); err != nil {
		return fmt.Errorf("failed to parse not_applied_all.json: %w", err)
	}
	if topN <= 0 {
		topN = 3
	}

	grouped := make(map[explainGroupKey][]report.NotAppliedRow)
	for _, row := range domainReport.Rows {
		key := explainGroupKey{Domain: row.Domain, Reason: row.Reason}
		grouped[key] = append(grouped[key], row)
	}

	keys := make([]explainGroupKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Domain != keys[j].Domain {
			return keys[i].Domain < keys[j].Domain
		}
		return keys[i].Reason < keys[j].Reason
	})

	for _, key := range keys {
		rows := grouped[key]
		fmt.Fprintf(stdoutWriter, "%s / %s (%d)\n", key.Domain, key.Reason, len(rows))
		limit := topN
		if len(rows) < limit {
			limit = len(rows)
		}
		for _, row := range rows[:limit] {
			fmt.Fprintf(stdoutWriter, "  - apple_id=%d artist=%q album=%q title=%q path=%q playlist=%q\n",
				row.AppleTrackID,
				row.AppleArtist,
				row.AppleAlbum,
				row.AppleName,
				firstNonEmpty(row.ApplePath, row.ApplePathRaw),
				row.PlaylistName,
			)
		}
	}
	return nil
}
