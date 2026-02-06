package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestNormalizeMatchPathModes(t *testing.T) {
	dashPath := "Radiohead/OK Computer/02 - Paranoid Android.mp3"
	noDashPath := "Radiohead/OK Computer/02 Paranoid Android.mp3"

	gotReal := normalizeMatchPathWithMode(dashPath, "", matchModeRealpath)
	wantReal := "radiohead/ok computer/02 - paranoid android.mp3"
	if gotReal != wantReal {
		t.Fatalf("realpath mode should keep dashes, got %q want %q", gotReal, wantReal)
	}

	gotLenient := normalizeMatchPathWithMode(dashPath, "", matchModeLenient)
	wantLenient := "radiohead/ok computer/02 paranoid android.mp3"
	if gotLenient != wantLenient {
		t.Fatalf("lenient mode should normalise dashes, got %q want %q", gotLenient, wantLenient)
	}

	gotLenientNoDash := normalizeMatchPathWithMode(noDashPath, "", matchModeLenient)
	if gotLenientNoDash != gotLenient {
		t.Fatalf("lenient mode should match dashed and non-dashed names, got %q want %q", gotLenientNoDash, gotLenient)
	}
}

func TestCoerceNonRootPath(t *testing.T) {
	got, warned := coerceNonRootPath("/")
	if got != "" {
		t.Fatalf("expected root to coerce to empty, got %q", got)
	}
	if !warned {
		t.Fatalf("expected warning flag for root coercion")
	}
}

func TestNormalizeMatchPathUnicode(t *testing.T) {
	nfc := "Music/étienne daho/ça.mp3"
	nfd := norm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatalf("expected NFC and NFD strings to differ")
	}

	gotNFC := normalizeMatchPathWithMode(nfc, "", matchModeRealpath)
	gotNFD := normalizeMatchPathWithMode(nfd, "", matchModeRealpath)
	if gotNFC != gotNFD {
		t.Fatalf("expected unicode-normalized paths to match, got %q vs %q", gotNFC, gotNFD)
	}
}

func TestNormalizeMatchPathUnicodeBjoerk(t *testing.T) {
	nfc := "Music/Björk/Debut/01 - Human Behaviour.mp3"
	nfd := norm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatalf("expected NFC and NFD strings to differ")
	}

	gotNFC := normalizeMatchPathWithMode(nfc, "", matchModeRealpath)
	gotNFD := normalizeMatchPathWithMode(nfd, "", matchModeRealpath)
	if gotNFC != gotNFD {
		t.Fatalf("expected unicode-normalized Björk paths to match, got %q vs %q", gotNFC, gotNFD)
	}
}

func TestNormalizeMusicRootPathFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "02 Paranoid Android.mp3")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create temp file: %s", err)
	}

	got := normalizeMusicRootPath(filePath)
	want := normalizeRootPath(dir)
	if got != want {
		t.Fatalf("expected file paths to coerce to dir, got %q want %q", got, want)
	}
}

func TestValidateNavidromePathWithEmptyMusicRoot(t *testing.T) {
	check := validateNavidromePath("/Volumes/Music/Radiohead/OK Computer/02 Paranoid Android.mp3", "", nil)
	if !check.isReal {
		t.Fatalf("expected absolute path to be real when music root is empty, got %q", check.reason)
	}
}

func TestBuildStarUpdates(t *testing.T) {
	pairs := map[string]*songPair{
		"a": {
			src: itunesInfo{loved: false, favorited: true, hasFav: true, id: 1},
			dst: subsonicInfo{id: "star-me", starred: false},
		},
		"b": {
			src: itunesInfo{loved: false, favorited: false, hasLoved: true, id: 2},
			dst: subsonicInfo{id: "unstar-me", starred: true},
		},
	}

	toStar, toUnstar := buildStarUpdates(pairs)
	if len(toStar) != 1 || !containsString(toStar, "star-me") {
		t.Fatalf("expected toStar to include star-me, got %v", toStar)
	}
	if len(toUnstar) != 1 || !containsString(toUnstar, "unstar-me") {
		t.Fatalf("expected toUnstar to include unstar-me, got %v", toUnstar)
	}
}

func containsString(values []string, value string) bool {
	for _, entry := range values {
		if entry == value {
			return true
		}
	}
	return false
}

func TestIsFavouritePreference(t *testing.T) {
	tests := []struct {
		name string
		info itunesInfo
		want bool
	}{
		{
			name: "favorited wins",
			info: itunesInfo{favorited: true, loved: false, hasFav: true, hasLoved: true},
			want: true,
		},
		{
			name: "loved fallback",
			info: itunesInfo{loved: true, hasLoved: true},
			want: true,
		},
		{
			name: "missing both",
			info: itunesInfo{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsFavourite(); got != tt.want {
				t.Fatalf("IsFavourite()=%t want %t", got, tt.want)
			}
		})
	}
}
