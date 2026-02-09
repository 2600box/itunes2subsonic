package main

import "flag"

func collectSetFlags() map[string]bool {
	setFlags := make(map[string]bool)
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})
	return setFlags
}

func applyDryRunOverrides(setFlags map[string]bool, apply bool) {
	if !apply {
		return
	}
	setFlags["dry_run"] = true
	*dryRun = false
}

func clearReportFlagsForApply(setFlags map[string]bool) {
	if !setFlags["report_library_stats"] {
		*reportLibrary = ""
	}
	if !setFlags["out_tsv"] {
		*reportOutTSV = ""
	}
	if !setFlags["report_sync_plan"] {
		*reportSyncPlan = ""
	}
	if !setFlags["report_sync_plan_tsv"] {
		*reportSyncPlanTSV = ""
	}
	if !setFlags["report_reconcile"] {
		*reportReconcile = ""
	}
	if !setFlags["report_remote_match_json"] {
		*reportRemoteMatchJSON = ""
	}
	if !setFlags["report_remote_match_tsv"] {
		*reportRemoteMatchTSV = ""
	}
	if !setFlags["report_remote_actionable_tsv"] {
		*reportRemoteActionable = ""
	}
	if !setFlags["report_remote_streaming_gaps"] {
		*reportRemoteStreaming = ""
	}
}
