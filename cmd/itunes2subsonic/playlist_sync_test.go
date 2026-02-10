package main

import (
	"errors"
	"io"
	"net/url"
	"testing"
	"time"
)

func TestUpdatePlaylistBatchedSkipsWhenPlaylistIDMissing(t *testing.T) {
	calls := 0
	err := updatePlaylistBatched(func(endpoint string, params url.Values) error {
		calls++
		return nil
	}, "", []string{"1", "2"}, 2)
	if !errors.Is(err, errMissingPlaylistID) {
		t.Fatalf("expected missing playlistId error, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no updatePlaylist calls, got %d", calls)
	}
}

func TestUpdatePlaylistBatchedSplitsIntoExpectedChunks(t *testing.T) {
	counts := make([]int, 0)
	ids := []string{"1", "2", "3", "4", "5", "6", "7"}
	err := updatePlaylistBatched(func(endpoint string, params url.Values) error {
		counts = append(counts, len(params["songIdToAdd"]))
		if params.Get("playlistId") != "pid-1" {
			t.Fatalf("expected playlistId to be set")
		}
		return nil
	}, "pid-1", ids, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(counts))
	}
	if counts[0] != 3 || counts[1] != 3 || counts[2] != 1 {
		t.Fatalf("unexpected batch sizes: %v", counts)
	}
}

func TestWithRetryOnlyRetriesTransientErrors(t *testing.T) {
	t.Run("transient retries then succeeds", func(t *testing.T) {
		attempts := 0
		err := withRetry(3, 0, func() error {
			attempts++
			if attempts < 3 {
				return io.EOF
			}
			return nil
		})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if attempts != 3 {
			t.Fatalf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("non transient stops immediately", func(t *testing.T) {
		attempts := 0
		err := withRetry(3, 0, func() error {
			attempts++
			return &subsonicAPIError{Code: 10, Message: "missing parameter: playlistId"}
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if attempts != 1 {
			t.Fatalf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("stops after max attempts", func(t *testing.T) {
		attempts := 0
		err := withRetry(3, time.Nanosecond, func() error {
			attempts++
			return io.EOF
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if attempts != 3 {
			t.Fatalf("expected 3 attempts, got %d", attempts)
		}
	})
}

func TestBuildDeletePlaylistParamsUsesIDKey(t *testing.T) {
	params := buildDeletePlaylistParams("playlist-123")
	if params.Get("id") != "playlist-123" {
		t.Fatalf("expected id to be set")
	}
	if params.Get("playlistId") != "" {
		t.Fatalf("did not expect playlistId key for deletePlaylist")
	}
}

func TestBuildUpdatePlaylistParamsUsesPlaylistIDKey(t *testing.T) {
	params := buildUpdatePlaylistParams("playlist-123", []string{"song-1"}, []string{"song-2"})
	if params.Get("playlistId") != "playlist-123" {
		t.Fatalf("expected playlistId to be set")
	}
	if got := params["songIdToAdd"]; len(got) != 1 || got[0] != "song-1" {
		t.Fatalf("unexpected songIdToAdd values: %v", got)
	}
	if got := params["songIdToRemove"]; len(got) != 1 || got[0] != "song-2" {
		t.Fatalf("unexpected songIdToRemove values: %v", got)
	}
}
