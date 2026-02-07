package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathOnDiskCaseMismatch(t *testing.T) {
	tempDir := t.TempDir()
	existingDir := filepath.Join(tempDir, "Awake is the New Sleep")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %s", err)
	}
	filePath := filepath.Join(existingDir, "02 Gamble Everything For Love.mp3")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	mismatched := filepath.Join(tempDir, "Awake Is the New Sleep", "02 Gamble Everything For Love.mp3")
	resolved, ok := resolvePathOnDisk(mismatched)
	if !ok {
		t.Fatalf("expected resolver to find existing file")
	}
	if resolved != filePath {
		t.Fatalf("expected resolved path %q, got %q", filePath, resolved)
	}
}

func TestResolvePathOnDiskUnicodeNormalization(t *testing.T) {
	tempDir := t.TempDir()
	dirName := "Beyonce\u0301"
	existingDir := filepath.Join(tempDir, dirName)
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %s", err)
	}
	filePath := filepath.Join(existingDir, "Track.mp3")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	requestedDir := "Beyoncé"
	mismatched := filepath.Join(tempDir, requestedDir, "Track.mp3")
	resolved, ok := resolvePathOnDisk(mismatched)
	if !ok {
		t.Fatalf("expected resolver to find existing file")
	}
	if resolved != filePath {
		t.Fatalf("expected resolved path %q, got %q", filePath, resolved)
	}
}
