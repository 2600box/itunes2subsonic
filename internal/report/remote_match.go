package report

type RemoteMatchStatus string

const (
	RemoteMatchStatusMatch         RemoteMatchStatus = "MATCH"
	RemoteMatchStatusLowConfidence RemoteMatchStatus = "LOW_CONFIDENCE"
	RemoteMatchStatusNoMatch       RemoteMatchStatus = "NO_MATCH"
)

type RemoteMatchCandidate struct {
	SongID      string  `json:"song_id"`
	Path        string  `json:"path,omitempty"`
	Artist      string  `json:"artist,omitempty"`
	Album       string  `json:"album,omitempty"`
	Title       string  `json:"title,omitempty"`
	Score       float64 `json:"score"`
	MatchMethod string  `json:"match_method,omitempty"`
}

type RemoteMatchEntry struct {
	AppleTrackID      int                    `json:"apple_track_id"`
	ApplePersistentID string                 `json:"apple_persistent_id,omitempty"`
	Loved             bool                   `json:"loved"`
	Rating            int                    `json:"rating,omitempty"`
	Artist            string                 `json:"artist,omitempty"`
	Album             string                 `json:"album,omitempty"`
	Title             string                 `json:"title,omitempty"`
	NormalizedArtist  string                 `json:"normalized_artist,omitempty"`
	NormalizedAlbum   string                 `json:"normalized_album,omitempty"`
	NormalizedTitle   string                 `json:"normalized_title,omitempty"`
	MatchStatus       RemoteMatchStatus      `json:"match_status"`
	MatchedSongID     string                 `json:"matched_navidrome_song_id,omitempty"`
	MatchedPath       string                 `json:"matched_path,omitempty"`
	MatchScore        float64                `json:"match_score"`
	MatchMethod       string                 `json:"match_method,omitempty"`
	CandidateCount    int                    `json:"candidate_count"`
	TopCandidates     []RemoteMatchCandidate `json:"top_candidates,omitempty"`
}

type RemoteMatchSummary struct {
	RemoteLovedTotal         int                `json:"remote_loved_total"`
	RemoteRatedTotal         int                `json:"remote_rated_total"`
	RemoteLovedAndRatedTotal int                `json:"remote_loved_and_rated_total"`
	MatchStatusCounts        map[string]int     `json:"match_status_counts"`
	LowConfidenceTop         []RemoteMatchEntry `json:"low_confidence_top"`
}

type RemoteMatchReport struct {
	SchemaVersion int                `json:"schema_version"`
	GeneratedAt   string             `json:"generated_at"`
	Version       string             `json:"version"`
	TopN          int                `json:"top_n"`
	Threshold     float64            `json:"threshold"`
	LowThreshold  float64            `json:"low_threshold"`
	Summary       RemoteMatchSummary `json:"summary"`
	Entries       []RemoteMatchEntry `json:"entries"`
}
