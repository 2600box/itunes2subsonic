package report

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type LibraryCounts struct {
	Total  int `json:"total"`
	Local  int `json:"local"`
	Remote int `json:"remote"`
}

type LibraryStats struct {
	Tracks        LibraryCounts `json:"tracks"`
	Loved         LibraryCounts `json:"loved"`
	Rated         LibraryCounts `json:"rated"`
	LovedAndRated LibraryCounts `json:"loved_and_rated"`
	LovedOnly     LibraryCounts `json:"loved_only"`
	RatedOnly     LibraryCounts `json:"rated_only"`
}

type PlanCountsBySource struct {
	Total  int `json:"total"`
	Local  int `json:"local"`
	Remote int `json:"remote"`
}

type PlanReasonCounts struct {
	Total    int            `json:"total"`
	ByReason map[string]int `json:"by_reason"`
}

type SyncPlanCounts struct {
	AppleTracks              LibraryCounts      `json:"apple_tracks"`
	AppleLoved               LibraryCounts      `json:"apple_loved"`
	AppleRated               LibraryCounts      `json:"apple_rated"`
	AppleLovedAndRated       LibraryCounts      `json:"apple_loved_and_rated"`
	PlannedStar              PlanCountsBySource `json:"planned_star"`
	PlannedUnstar            int                `json:"planned_unstar"`
	PlannedRatingsSet        int                `json:"planned_ratings_set"`
	PlannedRatingsUnset      int                `json:"planned_ratings_unset"`
	PlannedRatingsNoop       int                `json:"planned_ratings_noop"`
	PlannedPlaycountUpdates  int                `json:"planned_playcount_updates"`
	PlannedPlaycountNoop     int                `json:"planned_playcount_noop"`
	PlannedPlaylistCreates   int                `json:"planned_playlist_creates"`
	PlannedPlaylistUpdates   int                `json:"planned_playlist_updates"`
	PlannedPlaylistNoop      int                `json:"planned_playlist_noop"`
	PlannedPlaylistTrackAdds int                `json:"planned_playlist_track_adds"`
	PlannedPlaylistRemoves   int                `json:"planned_playlist_track_removes"`
	LovedNotApplied          PlanReasonCounts   `json:"loved_not_applied"`
	RatedNotApplied          PlanReasonCounts   `json:"rated_not_applied"`
}

type AppleTrack struct {
	TrackID   int    `json:"track_id"`
	Name      string `json:"name,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	TrackType string `json:"track_type,omitempty"`
	Rating    int    `json:"rating,omitempty"`
	Loved     bool   `json:"loved"`
	PathRaw   string `json:"path_raw,omitempty"`
	PathClean string `json:"path_clean,omitempty"`
	MatchKey  string `json:"match_key,omitempty"`
}

type NavidromeTrack struct {
	SongID string `json:"song_id,omitempty"`
	Path   string `json:"path,omitempty"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	Rating int    `json:"rating,omitempty"`
}

type LovedPlanEntry struct {
	Operation        string          `json:"operation"`
	Action           string          `json:"action"`
	Reason           string          `json:"reason,omitempty"`
	Apple            AppleTrack      `json:"apple"`
	Navidrome        *NavidromeTrack `json:"navidrome,omitempty"`
	NotAppliedReason string          `json:"not_applied_reason,omitempty"`
}

type RatingPlanEntry struct {
	Operation        string          `json:"operation"`
	Action           string          `json:"action"`
	Reason           string          `json:"reason,omitempty"`
	Apple            AppleTrack      `json:"apple"`
	Navidrome        *NavidromeTrack `json:"navidrome,omitempty"`
	DesiredRating    int             `json:"desired_rating,omitempty"`
	NotAppliedReason string          `json:"not_applied_reason,omitempty"`
}

type UnstarPlanEntry struct {
	Operation string         `json:"operation"`
	Action    string         `json:"action"`
	Navidrome NavidromeTrack `json:"navidrome"`
	Apple     *AppleTrack    `json:"apple_match,omitempty"`
	Reason    string         `json:"reason"`
}

type SyncPlanLoved struct {
	WillStar []LovedPlanEntry `json:"will_star"`
	Noop     []LovedPlanEntry `json:"noop"`
	WontStar []LovedPlanEntry `json:"wont_star"`
}

type SyncPlanUnstar struct {
	WillUnstar []UnstarPlanEntry `json:"will_unstar"`
	Noop       []UnstarPlanEntry `json:"noop,omitempty"`
	WontUnstar []UnstarPlanEntry `json:"wont_unstar,omitempty"`
}

type SyncPlanRatings struct {
	WillSet   []RatingPlanEntry `json:"will_set"`
	WillUnset []RatingPlanEntry `json:"will_unset"`
	Noop      []RatingPlanEntry `json:"noop"`
	WontSet   []RatingPlanEntry `json:"wont_set"`
}

type PlayCountPlanEntry struct {
	Operation            string          `json:"operation"`
	Action               string          `json:"action"`
	Reason               string          `json:"reason,omitempty"`
	Apple                AppleTrack      `json:"apple"`
	Navidrome            *NavidromeTrack `json:"navidrome,omitempty"`
	ApplePlayCount       int             `json:"apple_play_count,omitempty"`
	AppleLastPlayed      string          `json:"apple_last_played,omitempty"`
	NavidromePlayCount   int64           `json:"navidrome_play_count,omitempty"`
	DesiredScrobbleCount int64           `json:"desired_scrobble_count,omitempty"`
}

type PlaylistTrackRef struct {
	AppleTrackID    int    `json:"apple_track_id,omitempty"`
	NavidromeSongID string `json:"navidrome_song_id,omitempty"`
	Title           string `json:"title,omitempty"`
	Artist          string `json:"artist,omitempty"`
	Album           string `json:"album,omitempty"`
	Path            string `json:"path,omitempty"`
	PathRaw         string `json:"path_raw,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type PlaylistPlanEntry struct {
	Operation           string             `json:"operation"`
	Action              string             `json:"action"`
	Reason              string             `json:"reason,omitempty"`
	Name                string             `json:"name"`
	NavidromePlaylistID string             `json:"navidrome_playlist_id,omitempty"`
	AddTracks           []PlaylistTrackRef `json:"add_tracks,omitempty"`
	RemoveTracks        []PlaylistTrackRef `json:"remove_tracks,omitempty"`
	MissingTracks       []PlaylistTrackRef `json:"missing_tracks,omitempty"`
}

type SyncPlanPlayCounts struct {
	WillUpdate []PlayCountPlanEntry `json:"will_update"`
	Noop       []PlayCountPlanEntry `json:"noop"`
	WontUpdate []PlayCountPlanEntry `json:"wont_update"`
}

type SyncPlanPlaylists struct {
	Entries []PlaylistPlanEntry `json:"entries"`
}

type SyncPlan struct {
	SchemaVersion    int                `json:"schema_version"`
	GeneratedAt      string             `json:"generated_at"`
	NavidromeSummary NavidromeSummary   `json:"navidrome_summary"`
	Counts           SyncPlanCounts     `json:"counts"`
	Loved            SyncPlanLoved      `json:"loved"`
	Unstar           SyncPlanUnstar     `json:"unstar"`
	Ratings          SyncPlanRatings    `json:"ratings"`
	PlayCount        SyncPlanPlayCounts `json:"play_counts"`
	Playlists        SyncPlanPlaylists  `json:"playlists"`
}

type NavidromeSummary struct {
	TracksTotal  int `json:"navidrome_tracks_total"`
	StarredTotal int `json:"navidrome_starred_total"`
	RatedTotal   int `json:"navidrome_rated_total"`
}

type AppleDisaggregation struct {
	TracksTotal         int `json:"tracks_total"`
	TracksLocal         int `json:"tracks_local"`
	TracksRemote        int `json:"tracks_remote"`
	LovedTotal          int `json:"loved_total"`
	LovedLocal          int `json:"loved_local"`
	LovedRemote         int `json:"loved_remote"`
	RatedTotal          int `json:"rated_total"`
	RatedLocal          int `json:"rated_local"`
	RatedRemote         int `json:"rated_remote"`
	LovedAndRatedTotal  int `json:"loved_and_rated_total"`
	LovedAndRatedLocal  int `json:"loved_and_rated_local"`
	LovedAndRatedRemote int `json:"loved_and_rated_remote"`
	LovedOnlyTotal      int `json:"loved_only_total"`
	LovedOnlyLocal      int `json:"loved_only_local"`
	LovedOnlyRemote     int `json:"loved_only_remote"`
	RatedOnlyTotal      int `json:"rated_only_total"`
	RatedOnlyLocal      int `json:"rated_only_local"`
	RatedOnlyRemote     int `json:"rated_only_remote"`
}

type PlanCountsSummary struct {
	PlanStarCount        int `json:"plan_star_count"`
	PlanUnstarCount      int `json:"plan_unstar_count"`
	PlanRateSetCount     int `json:"plan_rate_set_count"`
	PlanRateUnsetCount   int `json:"plan_rate_unset_count"`
	PlanPlaycountCount   int `json:"plan_playcount_count"`
	PlanPlaylistOpsCount int `json:"plan_playlist_ops_count"`
}

type LovedReconcileSummary struct {
	AppleLovedLocal                     int            `json:"apple_loved_local"`
	NavidromeStarredTotal               int            `json:"navidrome_starred_total"`
	LovedAlreadyStarredInNavidromeCount int            `json:"loved_already_starred_in_navidrome_count"`
	PlanStarCount                       int            `json:"plan_star_count"`
	PlanUnappliedLovedCount             int            `json:"plan_unapplied_loved_count"`
	PlanUnappliedLovedByReason          map[string]int `json:"plan_unapplied_loved_by_reason"`
}

type ReconcileError struct {
	Message    string         `json:"message"`
	Expected   int            `json:"expected"`
	Actual     int            `json:"actual"`
	Components map[string]int `json:"components"`
}

type ReconcileReport struct {
	SchemaVersion       int                   `json:"schema_version"`
	GeneratedAt         string                `json:"generated_at"`
	Apple               AppleDisaggregation   `json:"apple"`
	Navidrome           NavidromeSummary      `json:"navidrome"`
	PlanCounts          PlanCountsSummary     `json:"plan_counts"`
	LovedRecon          LovedReconcileSummary `json:"loved_reconciliation"`
	PlanLovedNotApplied []LovedPlanEntry      `json:"plan_loved_not_applied"`
	PlanRatedNotApplied []RatingPlanEntry     `json:"plan_rated_not_applied"`
	ReconcileError      *ReconcileError       `json:"reconcile_error,omitempty"`
}

func WriteJSON(path string, payload interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func WriteTSV(path string, header []string, rows [][]string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if len(header) > 0 {
		if _, err := writer.WriteString(joinTSVRow(header)); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if _, err := writer.WriteString(joinTSVRow(row)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func joinTSVRow(fields []string) string {
	for i, value := range fields {
		fields[i] = sanitizeTSV(value)
	}
	return strings.Join(fields, "\t") + "\n"
}

func sanitizeTSV(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
