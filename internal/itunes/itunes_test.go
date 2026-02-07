package itunes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLibraryLovedFavorited(t *testing.T) {
	path := filepath.Join("testdata", "library.xml")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %s", err)
	}
	defer file.Close()

	library, err := LoadLibrary(file)
	if err != nil {
		t.Fatalf("failed to load library: %s", err)
	}

	track1 := library.Tracks["1"]
	if track1.Favorited == nil || !*track1.Favorited {
		t.Fatalf("expected track 1 to be favorited")
	}
	track2 := library.Tracks["2"]
	if track2.Loved == nil || !*track2.Loved {
		t.Fatalf("expected track 2 to be loved")
	}
	track3 := library.Tracks["3"]
	if track3.Loved == nil || !*track3.Loved {
		t.Fatalf("expected track 3 to be loved")
	}
}
