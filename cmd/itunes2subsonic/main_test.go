package main

import "testing"

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
