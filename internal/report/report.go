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
	Loved         LibraryCounts `json:"loved"`
	Rated         LibraryCounts `json:"rated"`
	LovedAndRated LibraryCounts `json:"loved_and_rated"`
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
	AppleLoved      LibraryCounts      `json:"apple_loved"`
	AppleRated      LibraryCounts      `json:"apple_rated"`
	PlannedStar     PlanCountsBySource `json:"planned_star"`
	PlannedUnstar   int                `json:"planned_unstar"`
	PlannedRatings  int                `json:"planned_ratings"`
	LovedNotApplied PlanReasonCounts   `json:"loved_not_applied"`
	RatedNotApplied PlanReasonCounts   `json:"rated_not_applied"`
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
	Apple            AppleTrack      `json:"apple"`
	Navidrome        *NavidromeTrack `json:"navidrome,omitempty"`
	NotAppliedReason string          `json:"not_applied_reason,omitempty"`
}

type RatingPlanEntry struct {
	Apple            AppleTrack      `json:"apple"`
	Navidrome        *NavidromeTrack `json:"navidrome,omitempty"`
	DesiredRating    int             `json:"desired_rating,omitempty"`
	NotAppliedReason string          `json:"not_applied_reason,omitempty"`
}

type UnstarPlanEntry struct {
	Navidrome NavidromeTrack `json:"navidrome"`
	Apple     *AppleTrack    `json:"apple_match,omitempty"`
	Reason    string         `json:"reason"`
}

type SyncPlanLoved struct {
	WillStar []LovedPlanEntry `json:"will_star"`
	WontStar []LovedPlanEntry `json:"wont_star"`
}

type SyncPlanUnstar struct {
	WillUnstar []UnstarPlanEntry `json:"will_unstar"`
	WontUnstar []UnstarPlanEntry `json:"wont_unstar,omitempty"`
}

type SyncPlanRatings struct {
	WillSet []RatingPlanEntry `json:"will_set"`
	WontSet []RatingPlanEntry `json:"wont_set"`
}

type SyncPlan struct {
	Counts  SyncPlanCounts  `json:"counts"`
	Loved   SyncPlanLoved   `json:"loved"`
	Unstar  SyncPlanUnstar  `json:"unstar"`
	Ratings SyncPlanRatings `json:"ratings"`
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
