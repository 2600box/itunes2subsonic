package match

import "testing"

func TestNormalizeText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"Beyoncé", "beyonce"},
		{"AC/DC", "ac dc"},
		{"Song Title (Remastered 2011)", "song title"},
		{"Hello - Remastered", "hello"},
		{"Artist feat. Someone", "artist someone"},
		{"The & That", "the and that"},
		{"  Live! [Demo] ", ""},
		{"Track (feat. Guest) - Bonus Track", "track"},
	}
	for _, tt := range tests {
		if got := NormalizeText(tt.input); got != tt.want {
			t.Fatalf("NormalizeText(%q)=%q want=%q", tt.input, got, tt.want)
		}
	}
}
