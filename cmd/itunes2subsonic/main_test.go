package main

import "testing"

func TestNormalizeMatchPathTrimsRootAndSeparators(t *testing.T) {
	srcPath := "/Users/me/Music/Radiohead/OK Computer/02 - Paranoid Android.mp3"
	dstPath := "Radiohead/OK Computer/02 - Paranoid Android.mp3"

	gotSrc := normalizeMatchPath(srcPath, "/Users/me/Music")
	gotDst := normalizeMatchPath(dstPath, "")

	if gotSrc != gotDst {
		t.Fatalf("expected normalized paths to match, got src=%q dst=%q", gotSrc, gotDst)
	}
	if gotSrc != "radiohead/ok computer/02 paranoid android.mp3" {
		t.Fatalf("unexpected normalized path: %q", gotSrc)
	}
}

func TestNormalizeMatchPathTrackDashVariants(t *testing.T) {
	withDash := "Radiohead/OK Computer/02 - Paranoid Android.mp3"
	noDash := "Radiohead/OK Computer/02 Paranoid Android.mp3"
	withTightDash := "Radiohead/OK Computer/02-Paranoid Android.mp3"
	withEnDash := "Radiohead/OK Computer/02 – Paranoid Android.mp3"

	gotDash := normalizeMatchPath(withDash, "")
	gotNoDash := normalizeMatchPath(noDash, "")
	gotTightDash := normalizeMatchPath(withTightDash, "")
	gotEnDash := normalizeMatchPath(withEnDash, "")

	if gotDash != gotNoDash {
		t.Fatalf("expected dash and no-dash paths to match, got dash=%q nodash=%q", gotDash, gotNoDash)
	}
	if gotDash != gotTightDash {
		t.Fatalf("expected tight dash and no-dash paths to match, got dash=%q tight=%q", gotDash, gotTightDash)
	}
	if gotDash != gotEnDash {
		t.Fatalf("expected en dash and no-dash paths to match, got dash=%q endash=%q", gotDash, gotEnDash)
	}
	if gotDash != "radiohead/ok computer/02 paranoid android.mp3" {
		t.Fatalf("unexpected normalized path: %q", gotDash)
	}
}
