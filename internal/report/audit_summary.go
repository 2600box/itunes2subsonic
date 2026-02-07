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
	Apple                 LibraryStats        `json:"apple"`
	Navidrome             NavidromeSummary    `json:"navidrome"`
	PlanCounts            SyncPlanCounts      `json:"plan_counts"`
	PredictedFinalStarred int                 `json:"predicted_final_starred"`
	Invariants            []AuditInvariant    `json:"invariants"`
	RemoteMatchSummary    *RemoteMatchSummary `json:"remote_match_summary,omitempty"`
}
