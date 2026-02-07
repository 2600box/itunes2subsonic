package report

type NotAppliedDomain string

const (
	NotAppliedDomainStars      NotAppliedDomain = "stars"
	NotAppliedDomainRatings    NotAppliedDomain = "ratings"
	NotAppliedDomainPlaycounts NotAppliedDomain = "playcounts"
	NotAppliedDomainPlaylists  NotAppliedDomain = "playlists"
	NotAppliedDomainAll        NotAppliedDomain = "all"
)

type NotAppliedRow struct {
	Domain          NotAppliedDomain `json:"domain"`
	Reason          string           `json:"reason"`
	AppleTrackID    int              `json:"apple_track_id,omitempty"`
	AppleName       string           `json:"apple_name,omitempty"`
	AppleArtist     string           `json:"apple_artist,omitempty"`
	AppleAlbum      string           `json:"apple_album,omitempty"`
	AppleTrackType  string           `json:"apple_track_type,omitempty"`
	ApplePath       string           `json:"apple_path,omitempty"`
	ApplePathRaw    string           `json:"apple_path_raw,omitempty"`
	NavidromeSongID string           `json:"navidrome_song_id,omitempty"`
	NavidromeTitle  string           `json:"navidrome_title,omitempty"`
	NavidromeArtist string           `json:"navidrome_artist,omitempty"`
	NavidromeAlbum  string           `json:"navidrome_album,omitempty"`
	NavidromePath   string           `json:"navidrome_path,omitempty"`
	PlaylistName    string           `json:"playlist_name,omitempty"`
}

type NotAppliedDomainSummary struct {
	Total    int            `json:"total"`
	ByReason map[string]int `json:"by_reason"`
}

type NotAppliedSummary struct {
	SchemaVersion     int                                          `json:"schema_version"`
	GeneratedAt       string                                       `json:"generated_at"`
	TotalRows         int                                          `json:"total_rows"`
	ByDomain          map[NotAppliedDomain]NotAppliedDomainSummary `json:"by_domain"`
	AggregateByReason map[string]int                               `json:"aggregate_by_reason"`
	SamplesByDomain   map[NotAppliedDomain][]NotAppliedRow         `json:"samples_by_domain,omitempty"`
}

type NotAppliedDomainReport struct {
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   string           `json:"generated_at"`
	Domain        NotAppliedDomain `json:"domain"`
	TotalRows     int              `json:"total_rows"`
	Truncated     bool             `json:"truncated"`
	Rows          []NotAppliedRow  `json:"rows"`
}
