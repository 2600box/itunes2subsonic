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
	if gotSrc != "radiohead/ok computer/02 - paranoid android.mp3" {
		t.Fatalf("unexpected normalized path: %q", gotSrc)
	}
}
