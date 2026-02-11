package reporting

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/logank/itunes2subsonic/internal/report"
	"github.com/logank/itunes2subsonic/pkg/match"
)

const (
	remoteStreamingGapSchemaVersion = 1
	matchThreshold                  = 0.92
	matchClearanceDelta             = 0.05
	durationToleranceSeconds        = 3
)

type RemoteStreamingAppleTrack struct {
	MatchKey        string
	TrackID         int
	Title           string
	Artist          string
	Album           string
	Rating          int
	Loved           bool
	TrackNumber     int
	DiscNumber      int
	DurationSeconds int
	Year            int
}

type RemoteStreamingNavidromeTrack struct {
	MatchKey        string
	SongID          string
	Title           string
	Artist          string
	Album           string
	Rating          int
	Starred         bool
	TrackNumber     int
	DiscNumber      int
	DurationSeconds int
	Year            int
}

type remoteStreamingIndexEntry struct {
	RemoteStreamingNavidromeTrack
	NormTitle  string
	NormArtist string
	NormAlbum  string
	TitleTok   []string
	ArtistTok  []string
	AlbumTok   []string
}

type remoteStreamingCandidate struct {
	remoteStreamingIndexEntry
	Score  float64
	Method string
}

func BuildRemoteStreamingGapReport(version string, apple []RemoteStreamingAppleTrack, navidrome []RemoteStreamingNavidromeTrack) report.RemoteStreamingGapReport {
	index := buildRemoteStreamingIndex(navidrome)
	appleTracks := make([]RemoteStreamingAppleTrack, len(apple))
	copy(appleTracks, apple)
	sort.Slice(appleTracks, func(i, j int) bool {
		a := appleTracks[i]
		b := appleTracks[j]
		if a.Artist != b.Artist {
			return a.Artist < b.Artist
		}
		if a.Album != b.Album {
			return a.Album < b.Album
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.TrackID < b.TrackID
	})

	entries := make([]report.RemoteStreamingGapEntry, 0, len(appleTracks))
	summary := report.RemoteStreamingGapSummary{}

	for _, track := range appleTracks {
		entry := buildRemoteStreamingGapEntry(track, index)
		entries = append(entries, entry)
		updateRemoteStreamingSummary(&summary, entry)
	}

	return report.RemoteStreamingGapReport{
		SchemaVersion: remoteStreamingGapSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Version:       version,
		Summary:       summary,
		Entries:       entries,
	}
}

func buildRemoteStreamingGapEntry(track RemoteStreamingAppleTrack, index remoteStreamingIndex) report.RemoteStreamingGapEntry {
	normTitle := match.NormalizeText(track.Title)
	normArtist := match.NormalizeText(track.Artist)
	normAlbum := match.NormalizeText(track.Album)

	candidates := index.candidates(track.MatchKey, normTitle, normArtist, normAlbum, track.DurationSeconds)
	scored := scoreRemoteStreamingCandidates(track, candidates, normTitle, normArtist, normAlbum)
	bestScore, secondScore := topScores(scored)

	status := report.RemoteStreamingGapStatusNoMatch
	confidence := string(report.RemoteStreamingGapStatusNoMatch)
	var bestCandidate *remoteStreamingCandidate
	if len(scored) > 0 && scored[0].Score >= matchThreshold {
		confidence = string(report.RemoteStreamingGapStatusMatch)
		if len(scored) == 1 || scored[0].Score-scored[1].Score >= matchClearanceDelta {
			status = report.RemoteStreamingGapStatusMatch
			bestCandidate = &scored[0]
		} else {
			status = report.RemoteStreamingGapStatusAmbiguous
			confidence = string(report.RemoteStreamingGapStatusAmbiguous)
		}
	}

	appleRating5 := ratingToFiveStar(track.Rating)
	entry := report.RemoteStreamingGapEntry{
		AppleTrackID:    track.TrackID,
		AppleTitle:      track.Title,
		AppleArtist:     track.Artist,
		AppleAlbum:      track.Album,
		AppleRating5:    appleRating5,
		AppleLoved:      track.Loved,
		MatchStatus:     status,
		MatchConfidence: confidence,
		ScoreBest:       bestScore,
		ScoreSecond:     secondScore,
	}

	if bestCandidate != nil {
		entry.NavidromeSongID = bestCandidate.SongID
		entry.NavidromeTitle = bestCandidate.Title
		entry.NavidromeArtist = bestCandidate.Artist
		entry.NavidromeAlbum = bestCandidate.Album
		entry.NavidromeRating100 = bestCandidate.Rating * 20
		entry.NavidromeStarred = bestCandidate.Starred
	}

	entry.GapFlags = buildRemoteStreamingGapFlags(track, appleRating5, status, bestCandidate)

	return entry
}

func buildRemoteStreamingGapFlags(track RemoteStreamingAppleTrack, appleRating5 int, status report.RemoteStreamingGapStatus, candidate *remoteStreamingCandidate) []string {
	flags := make([]string, 0)
	switch status {
	case report.RemoteStreamingGapStatusNoMatch:
		flags = append(flags, "missing_in_navidrome")
		if track.Loved {
			flags = append(flags, "loved_missing_in_navidrome")
		}
		if appleRating5 > 0 {
			flags = append(flags, "rating_missing_in_navidrome")
		}
	case report.RemoteStreamingGapStatusMatch:
		if candidate == nil {
			break
		}
		if track.Loved && !candidate.Starred {
			flags = append(flags, "loved_not_starred")
		}
		if appleRating5 > 0 {
			if candidate.Rating == 0 {
				flags = append(flags, "rating_missing")
			} else if candidate.Rating != appleRating5 {
				flags = append(flags, "rating_diff")
			}
		}
	}
	return flags
}

func updateRemoteStreamingSummary(summary *report.RemoteStreamingGapSummary, entry report.RemoteStreamingGapEntry) {
	summary.TotalTracks++
	switch entry.MatchStatus {
	case report.RemoteStreamingGapStatusMatch:
		summary.MatchCount++
	case report.RemoteStreamingGapStatusAmbiguous:
		summary.AmbiguousCount++
	case report.RemoteStreamingGapStatusNoMatch:
		summary.NoMatchCount++
	}

	flagSet := make(map[string]struct{}, len(entry.GapFlags))
	for _, flag := range entry.GapFlags {
		flagSet[flag] = struct{}{}
	}
	if _, ok := flagSet["missing_in_navidrome"]; ok {
		summary.MissingInNavidromeCount++
	}
	if _, ok := flagSet["loved_missing_in_navidrome"]; ok {
		summary.LovedMissingInNavidromeCount++
	}
	if _, ok := flagSet["rating_missing_in_navidrome"]; ok {
		summary.RatingMissingInNavidromeCount++
	}
	if _, ok := flagSet["loved_not_starred"]; ok {
		summary.LovedNotStarredCount++
	}
	if _, ok := flagSet["rating_diff"]; ok {
		summary.RatingDiffCount++
	}
	if _, ok := flagSet["rating_missing"]; ok {
		summary.RatingMissingCount++
	}
	if entry.MatchStatus == report.RemoteStreamingGapStatusMatch {
		if len(entry.GapFlags) == 0 {
			summary.AlignedCount++
		} else {
			summary.PresentButMissingMetadataCount++
		}
	}
}

type remoteStreamingIndex struct {
	byMatchKey  map[string][]remoteStreamingIndexEntry
	exact       map[string][]remoteStreamingIndexEntry
	titleArtist map[string][]remoteStreamingIndexEntry
	titleAlbum  map[string][]remoteStreamingIndexEntry
}

func buildRemoteStreamingIndex(navidrome []RemoteStreamingNavidromeTrack) remoteStreamingIndex {
	index := remoteStreamingIndex{
		byMatchKey:  make(map[string][]remoteStreamingIndexEntry),
		exact:       make(map[string][]remoteStreamingIndexEntry),
		titleArtist: make(map[string][]remoteStreamingIndexEntry),
		titleAlbum:  make(map[string][]remoteStreamingIndexEntry),
	}
	for _, track := range navidrome {
		normTitle := match.NormalizeText(track.Title)
		normArtist := match.NormalizeText(track.Artist)
		normAlbum := match.NormalizeText(track.Album)
		entry := remoteStreamingIndexEntry{
			RemoteStreamingNavidromeTrack: track,
			NormTitle:                     normTitle,
			NormArtist:                    normArtist,
			NormAlbum:                     normAlbum,
			TitleTok:                      match.Tokens(normTitle),
			ArtistTok:                     match.Tokens(normArtist),
			AlbumTok:                      match.Tokens(normAlbum),
		}
		if track.MatchKey != "" {
			index.byMatchKey[track.MatchKey] = append(index.byMatchKey[track.MatchKey], entry)
		}
		if normTitle != "" && normArtist != "" && normAlbum != "" {
			key := compositeKey(normTitle, normArtist, normAlbum)
			index.exact[key] = append(index.exact[key], entry)
		}
		if normTitle != "" && normArtist != "" {
			key := compositeKey(normTitle, normArtist)
			index.titleArtist[key] = append(index.titleArtist[key], entry)
		}
		if normTitle != "" && normAlbum != "" {
			key := compositeKey(normTitle, normAlbum)
			index.titleAlbum[key] = append(index.titleAlbum[key], entry)
		}
	}
	return index
}

func (index remoteStreamingIndex) candidates(matchKey string, normTitle string, normArtist string, normAlbum string, durationSeconds int) []remoteStreamingIndexEntry {
	if matchKey != "" {
		if candidates := index.byMatchKey[matchKey]; len(candidates) > 0 {
			return filterDurationCandidates(candidates, durationSeconds)
		}
	}
	if normTitle == "" {
		return nil
	}
	if normArtist != "" && normAlbum != "" {
		key := compositeKey(normTitle, normArtist, normAlbum)
		if candidates := index.exact[key]; len(candidates) > 0 {
			return candidates
		}
	}
	if normArtist != "" {
		key := compositeKey(normTitle, normArtist)
		candidates := index.titleArtist[key]
		return filterDurationCandidates(candidates, durationSeconds)
	}
	if normAlbum != "" && isGarbageArtist(normArtist) {
		key := compositeKey(normTitle, normAlbum)
		candidates := index.titleAlbum[key]
		return filterDurationCandidates(candidates, durationSeconds)
	}
	return nil
}

func filterDurationCandidates(candidates []remoteStreamingIndexEntry, durationSeconds int) []remoteStreamingIndexEntry {
	if durationSeconds <= 0 {
		return candidates
	}
	filtered := make([]remoteStreamingIndexEntry, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.DurationSeconds <= 0 {
			filtered = append(filtered, candidate)
			continue
		}
		diff := int(math.Abs(float64(candidate.DurationSeconds - durationSeconds)))
		if diff <= durationToleranceSeconds {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func scoreRemoteStreamingCandidates(track RemoteStreamingAppleTrack, candidates []remoteStreamingIndexEntry, normTitle string, normArtist string, normAlbum string) []remoteStreamingCandidate {
	if len(candidates) == 0 {
		return nil
	}
	artistTokens := match.Tokens(normArtist)
	albumTokens := match.Tokens(normAlbum)
	titleTokens := match.Tokens(normTitle)
	scored := make([]remoteStreamingCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		artistExact := candidate.NormArtist == normArtist && normArtist != ""
		albumExact := candidate.NormAlbum == normAlbum && normAlbum != ""
		titleExact := candidate.NormTitle == normTitle && normTitle != ""
		score, method := match.ScoreComposite(artistTokens, albumTokens, titleTokens, candidate.ArtistTok, candidate.AlbumTok, candidate.TitleTok, artistExact, albumExact, titleExact)
		score = applyMetadataBonuses(score, track, candidate)
		scored = append(scored, remoteStreamingCandidate{
			remoteStreamingIndexEntry: candidate,
			Score:                     score,
			Method:                    method,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].NormArtist != scored[j].NormArtist {
			return scored[i].NormArtist < scored[j].NormArtist
		}
		if scored[i].NormAlbum != scored[j].NormAlbum {
			return scored[i].NormAlbum < scored[j].NormAlbum
		}
		if scored[i].NormTitle != scored[j].NormTitle {
			return scored[i].NormTitle < scored[j].NormTitle
		}
		return scored[i].SongID < scored[j].SongID
	})
	return scored
}

func applyMetadataBonuses(base float64, track RemoteStreamingAppleTrack, candidate remoteStreamingIndexEntry) float64 {
	score := base
	if track.TrackNumber > 0 && candidate.TrackNumber > 0 && track.TrackNumber == candidate.TrackNumber {
		score += 0.03
	}
	if track.DiscNumber > 0 && candidate.DiscNumber > 0 && track.DiscNumber == candidate.DiscNumber {
		score += 0.02
	}
	if track.Year > 0 && candidate.Year > 0 && track.Year == candidate.Year {
		score += 0.01
	}
	if track.DurationSeconds > 0 && candidate.DurationSeconds > 0 {
		diff := int(math.Abs(float64(track.DurationSeconds - candidate.DurationSeconds)))
		switch {
		case diff <= 1:
			score += 0.03
		case diff <= durationToleranceSeconds:
			score += 0.02
		case diff <= 5:
			score += 0.01
		}
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func topScores(scored []remoteStreamingCandidate) (float64, float64) {
	if len(scored) == 0 {
		return 0, 0
	}
	if len(scored) == 1 {
		return scored[0].Score, 0
	}
	return scored[0].Score, scored[1].Score
}

func compositeKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func ratingToFiveStar(rating int) int {
	if rating <= 0 {
		return 0
	}
	return rating / 20
}

func isGarbageArtist(norm string) bool {
	if norm == "" {
		return true
	}
	switch norm {
	case "unknown", "various", "various artists":
		return true
	default:
		return false
	}
}
