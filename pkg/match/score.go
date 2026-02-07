package match

import (
	"math"
	"sort"
)

const (
	weightTitle  = 0.45
	weightArtist = 0.35
	weightAlbum  = 0.20
)

func TokenJaccard(a []string, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, token := range a {
		setA[token] = struct{}{}
	}
	intersection := 0
	setB := make(map[string]struct{}, len(b))
	for _, token := range b {
		setB[token] = struct{}{}
		if _, ok := setA[token]; ok {
			intersection++
		}
	}
	union := len(setA)
	for token := range setB {
		if _, ok := setA[token]; !ok {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func ScoreComposite(artistTokens []string, albumTokens []string, titleTokens []string, candidateArtist []string, candidateAlbum []string, candidateTitle []string, artistExact bool, albumExact bool, titleExact bool) (float64, string) {
	artistScore := TokenJaccard(artistTokens, candidateArtist)
	albumScore := TokenJaccard(albumTokens, candidateAlbum)
	titleScore := TokenJaccard(titleTokens, candidateTitle)
	composite := weightTitle*titleScore + weightArtist*artistScore + weightAlbum*albumScore
	method := "COMPOSITE"
	if artistExact && albumExact && titleExact {
		composite = math.Max(composite, 1.0)
		method = "EXACT_NORM"
	} else if titleExact && artistExact {
		composite = math.Max(composite, 0.98)
		method = "EXACT_TITLE_ARTIST"
	} else if titleExact {
		composite = math.Max(composite, 0.94)
		method = "EXACT_TITLE"
	}
	return composite, method
}

func compareCandidates(a Candidate, b Candidate) int {
	if a.Score != b.Score {
		if a.Score > b.Score {
			return -1
		}
		return 1
	}
	if a.Path != b.Path {
		return compareString(a.Path, b.Path)
	}
	if a.SongID != b.SongID {
		return compareString(a.SongID, b.SongID)
	}
	if a.Artist != b.Artist {
		return compareString(a.Artist, b.Artist)
	}
	if a.Album != b.Album {
		return compareString(a.Album, b.Album)
	}
	if a.Title != b.Title {
		return compareString(a.Title, b.Title)
	}
	if a.NormArtist != b.NormArtist {
		return compareString(a.NormArtist, b.NormArtist)
	}
	if a.NormAlbum != b.NormAlbum {
		return compareString(a.NormAlbum, b.NormAlbum)
	}
	if a.NormTitle != b.NormTitle {
		return compareString(a.NormTitle, b.NormTitle)
	}
	return 0
}

func compareString(a string, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func StableSortCandidates(candidates []Candidate, topN int) []Candidate {
	// Ensure a deterministic total order for candidates regardless of input order.
	sort.Slice(candidates, func(i, j int) bool {
		return compareCandidates(candidates[i], candidates[j]) < 0
	})
	if topN <= 0 || len(candidates) <= topN {
		return candidates
	}
	return candidates[:topN]
}
