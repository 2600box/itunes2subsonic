package match

import (
	"math/rand"
	"testing"
)

func TestTokenJaccard(t *testing.T) {
	t.Parallel()
	a := []string{"one", "two", "three"}
	b := []string{"two", "three", "four"}
	got := TokenJaccard(a, b)
	if got < 0.49 || got > 0.51 {
		t.Fatalf("TokenJaccard=%f want ~0.5", got)
	}
}

func TestScoreCompositeExact(t *testing.T) {
	t.Parallel()
	artist := Tokens(NormalizeText("Artist"))
	album := Tokens(NormalizeText("Album"))
	title := Tokens(NormalizeText("Song"))
	score, method := ScoreComposite(artist, album, title, artist, album, title, true, true, true)
	if score < 1.0 {
		t.Fatalf("ScoreComposite exact=%f want=1.0", score)
	}
	if method != "EXACT_NORM" {
		t.Fatalf("ScoreComposite method=%s want=EXACT_NORM", method)
	}
}

func TestStableSortCandidatesDeterministic(t *testing.T) {
	t.Parallel()
	candidates := []Candidate{
		{SongID: "b", Path: "/zz/longer.flac", Score: 0.9},
		{SongID: "a", Path: "/aa/short.flac", Score: 0.9},
		{SongID: "c", Path: "/aa/shorter.flac", Score: 0.9},
	}
	ordered := StableSortCandidates(candidates, 0)
	if ordered[0].SongID != "a" {
		t.Fatalf("first candidate=%s want=a", ordered[0].SongID)
	}
	if ordered[1].SongID != "c" {
		t.Fatalf("second candidate=%s want=c", ordered[1].SongID)
	}
}

func TestStableSortCandidatesShuffledInput(t *testing.T) {
	t.Parallel()
	base := []Candidate{
		{SongID: "b", Path: "/zz/longer.flac", Score: 0.9},
		{SongID: "a", Path: "/aa/short.flac", Score: 0.9},
		{SongID: "c", Path: "/aa/shorter.flac", Score: 0.9},
		{SongID: "d", Path: "/aa/shorter.flac", Score: 0.85},
	}
	expected := []string{"a", "c", "b", "d"}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 25; i++ {
		shuffled := append([]Candidate(nil), base...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		ordered := StableSortCandidates(shuffled, 0)
		if got := candidateIDs(ordered); !equalStrings(got, expected) {
			t.Fatalf("shuffle %d ordered=%v want=%v", i, got, expected)
		}
	}
}

func TestStableSortCandidatesMapIteration(t *testing.T) {
	t.Parallel()
	candidateMap := map[string]Candidate{
		"b": {SongID: "b", Path: "/zz/longer.flac", Score: 0.9},
		"a": {SongID: "a", Path: "/aa/short.flac", Score: 0.9},
		"c": {SongID: "c", Path: "/aa/shorter.flac", Score: 0.9},
		"d": {SongID: "d", Path: "/aa/shorter.flac", Score: 0.85},
	}
	expected := []string{"a", "c", "b", "d"}
	for i := 0; i < 25; i++ {
		candidates := make([]Candidate, 0, len(candidateMap))
		for _, candidate := range candidateMap {
			candidates = append(candidates, candidate)
		}
		ordered := StableSortCandidates(candidates, 0)
		if got := candidateIDs(ordered); !equalStrings(got, expected) {
			t.Fatalf("iteration %d ordered=%v want=%v", i, got, expected)
		}
	}
}

func TestStableSortCandidatesUsesNormalizedTieBreakers(t *testing.T) {
	t.Parallel()
	candidates := []Candidate{
		{SongID: "a", Path: "/zz/longer.flac", Score: 0.9, NormArtist: "beta"},
		{SongID: "b", Path: "/aa/short.flac", Score: 0.9, NormArtist: "alpha"},
	}
	ordered := StableSortCandidates(candidates, 0)
	if ordered[0].SongID != "b" {
		t.Fatalf("expected normalized artist tie-breaker to sort first, got %s", ordered[0].SongID)
	}
}

func TestCompareCandidatesTotalOrder(t *testing.T) {
	t.Parallel()
	candidates := []Candidate{
		{SongID: "a", Path: "/aa/short.flac", Score: 0.9, Artist: "Artist", Title: "Song"},
		{SongID: "b", Path: "/zz/longer.flac", Score: 0.9, Artist: "Artist", Title: "Song"},
		{SongID: "c", Path: "/aa/shorter.flac", Score: 0.9, Artist: "Artist", Title: "Song"},
		{SongID: "d", Path: "/aa/shorter.flac", Score: 0.9, Artist: "Artist", Title: "Song", NormTitle: "song"},
		{SongID: "e", Path: "", Score: 0.9, Artist: "Artist", Title: "Song"},
		{SongID: "f", Path: "", Score: 0.85, Artist: "Artist", Title: "Song"},
	}
	for i, a := range candidates {
		for j, b := range candidates {
			if i == j {
				continue
			}
			ab := compareCandidates(a, b)
			ba := compareCandidates(b, a)
			if ab == 0 && ba != 0 {
				t.Fatalf("compareCandidates not symmetric for %v vs %v", a.SongID, b.SongID)
			}
			if ab < 0 && ba <= 0 {
				t.Fatalf("compareCandidates inconsistent for %v vs %v", a.SongID, b.SongID)
			}
			if ab > 0 && ba >= 0 {
				t.Fatalf("compareCandidates inconsistent for %v vs %v", a.SongID, b.SongID)
			}
		}
	}
}

func candidateIDs(candidates []Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.SongID)
	}
	return ids
}

func equalStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
