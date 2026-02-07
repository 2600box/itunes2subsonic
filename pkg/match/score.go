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

func StableSortCandidates(candidates []Candidate, topN int) []Candidate {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if len(candidates[i].Path) != len(candidates[j].Path) {
			return len(candidates[i].Path) < len(candidates[j].Path)
		}
		if candidates[i].Path != candidates[j].Path {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].SongID < candidates[j].SongID
	})
	if topN <= 0 || len(candidates) <= topN {
		return candidates
	}
	return candidates[:topN]
}
