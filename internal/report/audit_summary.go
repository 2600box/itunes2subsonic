package report

type AuditInvariant struct {
	Name    string         `json:"name"`
	Left    int            `json:"left"`
	Right   int            `json:"right"`
	Passed  bool           `json:"passed"`
	Details map[string]int `json:"details,omitempty"`
}

type AuditSummary struct {
	SchemaVersion         int                 `json:"schema_version"`
	GeneratedAt           string              `json:"generated_at"`
	Version               string              `json:"version"`
	GitCommit             string              `json:"git_commit,omitempty"`
	Inputs                AuditInputs         `json:"inputs"`
	Artifacts             AuditArtifacts      `json:"artifacts"`
	Apple                 LibraryStats        `json:"apple"`
	Navidrome             NavidromeSummary    `json:"navidrome"`
	PlanCounts            SyncPlanCounts      `json:"plan_counts"`
	NotAppliedSummary     NotAppliedSummary   `json:"not_applied_summary"`
	PredictedFinalStarred int                 `json:"predicted_final_starred"`
	Invariants            []AuditInvariant    `json:"invariants"`
	RemoteMatchSummary    *RemoteMatchSummary `json:"remote_match_summary,omitempty"`
}

type AuditInputs struct {
	ItunesXML       string   `json:"itunes_xml"`
	MusicRoot       string   `json:"music_root"`
	SubsonicURL     string   `json:"subsonic_url"`
	MatchMode       string   `json:"match_mode"`
	RequireRealPath bool     `json:"require_real_path"`
	Extensions      []string `json:"extensions"`
	RunDir          string   `json:"run_dir"`
	ReportOnly      bool     `json:"report_only"`
}

type AuditArtifacts struct {
	NavidromeDump            string `json:"navidrome_dump"`
	SyncPlan                 string `json:"sync_plan"`
	Reconcile                string `json:"reconcile"`
	SyncPlanTSVBase          string `json:"sync_plan_tsv_base"`
	NavidromeStarredTSV      string `json:"navidrome_starred_baseline_tsv"`
	AuditSummaryJSON         string `json:"audit_summary_json"`
	AuditSummaryTSV          string `json:"audit_summary_tsv"`
	NotAppliedSummaryJSON    string `json:"not_applied_summary_json"`
	NotAppliedAllTSV         string `json:"not_applied_all_tsv"`
	NotAppliedAllJSON        string `json:"not_applied_all_json"`
	NotAppliedStarsTSV       string `json:"not_applied_stars_tsv"`
	NotAppliedStarsJSON      string `json:"not_applied_stars_json"`
	NotAppliedRatingsTSV     string `json:"not_applied_ratings_tsv"`
	NotAppliedRatingsJSON    string `json:"not_applied_ratings_json"`
	NotAppliedPlaycountsTSV  string `json:"not_applied_playcounts_tsv"`
	NotAppliedPlaycountsJSON string `json:"not_applied_playcounts_json"`
	NotAppliedPlaylistsTSV   string `json:"not_applied_playlists_tsv"`
	NotAppliedPlaylistsJSON  string `json:"not_applied_playlists_json"`
	RemoteMatchJSON          string `json:"remote_match_json,omitempty"`
	RemoteMatchTSV           string `json:"remote_match_tsv,omitempty"`
}
