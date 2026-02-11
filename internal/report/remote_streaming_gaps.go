package report

type RemoteStreamingGapStatus string

const (
	RemoteStreamingGapStatusMatch     RemoteStreamingGapStatus = "MATCH"
	RemoteStreamingGapStatusAmbiguous RemoteStreamingGapStatus = "AMBIGUOUS"
	RemoteStreamingGapStatusNoMatch   RemoteStreamingGapStatus = "NO_MATCH"
)

type RemoteStreamingGapEntry struct {
	MatchConfidence    string                   `json:"match_confidence,omitempty"`
	AppleTrackID       int                      `json:"apple_track_id"`
	AppleTitle         string                   `json:"apple_title,omitempty"`
	AppleArtist        string                   `json:"apple_artist,omitempty"`
	AppleAlbum         string                   `json:"apple_album,omitempty"`
	AppleRating5       int                      `json:"apple_rating_5,omitempty"`
	AppleLoved         bool                     `json:"apple_loved"`
	MatchStatus        RemoteStreamingGapStatus `json:"match_status"`
	NavidromeSongID    string                   `json:"navidrome_song_id,omitempty"`
	NavidromeTitle     string                   `json:"navidrome_title,omitempty"`
	NavidromeArtist    string                   `json:"navidrome_artist,omitempty"`
	NavidromeAlbum     string                   `json:"navidrome_album,omitempty"`
	NavidromeRating100 int                      `json:"navidrome_rating_100,omitempty"`
	NavidromeStarred   bool                     `json:"navidrome_starred,omitempty"`
	GapFlags           []string                 `json:"gap_flags,omitempty"`
	ScoreBest          float64                  `json:"score_best,omitempty"`
	ScoreSecond        float64                  `json:"score_second,omitempty"`
}

type RemoteStreamingGapSummary struct {
	TotalTracks                    int `json:"total_tracks"`
	MatchCount                     int `json:"match_count"`
	AmbiguousCount                 int `json:"ambiguous_count"`
	NoMatchCount                   int `json:"no_match_count"`
	MissingInNavidromeCount        int `json:"missing_in_navidrome_count"`
	PresentButMissingMetadataCount int `json:"present_but_missing_metadata_count"`
	AlignedCount                   int `json:"aligned_count"`
	LovedMissingInNavidromeCount   int `json:"loved_missing_in_navidrome_count"`
	RatingMissingInNavidromeCount  int `json:"rating_missing_in_navidrome_count"`
	LovedNotStarredCount           int `json:"loved_not_starred_count"`
	RatingDiffCount                int `json:"rating_diff_count"`
	RatingMissingCount             int `json:"rating_missing_count"`
}

type RemoteStreamingGapReport struct {
	SchemaVersion int                       `json:"schema_version"`
	GeneratedAt   string                    `json:"generated_at"`
	Version       string                    `json:"version"`
	Summary       RemoteStreamingGapSummary `json:"summary"`
	Entries       []RemoteStreamingGapEntry `json:"entries"`
}
