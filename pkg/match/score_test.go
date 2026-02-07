package match

import "testing"

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
