package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/delucks/go-subsonic"
	"github.com/logank/itunes2subsonic/internal/report"
)

const (
	subsonicOKResponse = `<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.8.0"></subsonic-response>`
)

func TestReportSyncPlanReportOnlyWritesArtifacts(t *testing.T) {
	t.Helper()

	server := newSubsonicTestServer()
	defer server.Close()

	client := &subsonic.Client{
		Client:     server.Client(),
		BaseUrl:    server.URL,
		User:       "tester",
		ClientName: "itunes2subsonic-test",
	}
	if err := client.Authenticate("pass"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "sync_plan.json")
	reconcilePath := filepath.Join(tempDir, "reconcile.json")
	filters := filterOptions{path: "Music"}
	allowlist := map[string]struct{}{".mp3": {}}

	resetFlags := setTestFlags(map[*string]string{
		dumpFile:     filepath.Join("testdata", "navidrome_dump.json"),
		itunesRoot:   "",
		subsonicRoot: "",
		matchMode:    string(matchModeRealpath),
	})
	defer resetFlags()
	resetBoolFlags := setTestBoolFlags(map[*bool]bool{
		verifySrcFiles: false,
		copyUnrated:    false,
		updatePlay:     false,
	})
	defer resetBoolFlags()

	if err := runReportSyncPlan(client, filepath.Join("testdata", "itunes_tiny.xml"), planPath, filters, allowlist, matchModeRealpath, true, true); err != nil {
		t.Fatalf("runReportSyncPlan: %v", err)
	}
	if err := runReportReconcile(filepath.Join("testdata", "itunes_tiny.xml"), planPath, reconcilePath, filters, false); err != nil {
		t.Fatalf("runReportReconcile: %v", err)
	}

	assertFileExists(t, planPath)
	assertFileExists(t, reconcilePath)

	plan := readPlan(t, planPath)
	planStarRows := countTSVRows(t, filepath.Join(tempDir, "plan_star.tsv"))
	planUnstarRows := countTSVRows(t, filepath.Join(tempDir, "plan_unstar.tsv"))
	unappliedLovedRows := countTSVRows(t, filepath.Join(tempDir, "unapplied_loved.tsv"))

	if planStarRows != len(plan.Loved.WillStar) {
		t.Fatalf("plan_star.tsv rows=%d want=%d", planStarRows, len(plan.Loved.WillStar))
	}
	if planUnstarRows != len(plan.Unstar.WillUnstar) {
		t.Fatalf("plan_unstar.tsv rows=%d want=%d", planUnstarRows, len(plan.Unstar.WillUnstar))
	}
	if unappliedLovedRows != len(plan.Loved.WontStar) {
		t.Fatalf("unapplied_loved.tsv rows=%d want=%d", unappliedLovedRows, len(plan.Loved.WontStar))
	}

	reconcile := readReconcile(t, reconcilePath)
	if reconcile.ReconcileError != nil {
		t.Fatalf("unexpected reconcile error: %s", reconcile.ReconcileError.Message)
	}
	if reconcile.LovedRecon.AppleLovedLocal != 3 {
		t.Fatalf("apple loved local=%d want=3", reconcile.LovedRecon.AppleLovedLocal)
	}
}

func TestReportSyncPlanLiveWritesArtifacts(t *testing.T) {
	t.Helper()

	server := newSubsonicTestServer()
	defer server.Close()

	client := &subsonic.Client{
		Client:     server.Client(),
		BaseUrl:    server.URL,
		User:       "tester",
		ClientName: "itunes2subsonic-test",
	}
	if err := client.Authenticate("pass"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "sync_plan.json")
	reconcilePath := filepath.Join(tempDir, "reconcile.json")
	filters := filterOptions{}
	allowlist := map[string]struct{}{".mp3": {}}

	resetFlags := setTestFlags(map[*string]string{
		dumpFile:     "",
		itunesRoot:   "",
		subsonicRoot: "",
		matchMode:    string(matchModeRealpath),
	})
	defer resetFlags()
	resetBoolFlags := setTestBoolFlags(map[*bool]bool{
		verifySrcFiles: false,
		copyUnrated:    false,
		updatePlay:     false,
	})
	defer resetBoolFlags()

	if err := runReportSyncPlan(client, filepath.Join("testdata", "itunes_tiny.xml"), planPath, filters, allowlist, matchModeRealpath, false, false); err != nil {
		t.Fatalf("runReportSyncPlan: %v", err)
	}
	if err := runReportReconcile(filepath.Join("testdata", "itunes_tiny.xml"), planPath, reconcilePath, filters, false); err != nil {
		t.Fatalf("runReportReconcile: %v", err)
	}

	assertFileExists(t, planPath)
	assertFileExists(t, reconcilePath)
}

func newSubsonicTestServer() *httptest.Server {
	starred := []string{
		`<song id="song1" title="Track 1" artist="Artist 1" album="Album 1" path="/Music/Artist1/Album1/Track1.mp3" userRating="0" playCount="0" />`,
		`<song id="song3" title="Track 3" artist="Artist 2" album="Album 2" path="/Music/Artist2/Album2/Track3.mp3" userRating="0" playCount="0" />`,
	}
	searchSongs := []string{
		`<song id="song1" title="Track 1" artist="Artist 1" album="Album 1" path="/Music/Artist1/Album1/Track1.mp3" userRating="0" playCount="0" />`,
		`<song id="song2" title="Track 2" artist="Artist 1" album="Album 1" path="/Music/Artist1/Album1/Track2.mp3" userRating="0" playCount="0" />`,
		`<song id="song3" title="Track 3" artist="Artist 2" album="Album 2" path="/Music/Artist2/Album2/Track3.mp3" userRating="0" playCount="0" />`,
	}

	mux := http.NewServeMux()
	pingHandler := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, subsonicOKResponse)
	}
	mux.HandleFunc("/rest/ping", pingHandler)
	mux.HandleFunc("/rest/ping.view", pingHandler)

	starredHandler := func(w http.ResponseWriter, _ *http.Request) {
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.8.0">
  <starred2>
    %s
  </starred2>
</subsonic-response>`, strings.Join(starred, "\n    "))
		_, _ = fmt.Fprint(w, body)
	}
	mux.HandleFunc("/rest/getStarred2", starredHandler)
	mux.HandleFunc("/rest/getStarred2.view", starredHandler)

	searchHandler := func(w http.ResponseWriter, r *http.Request) {
		offset := 0
		if value := r.URL.Query().Get("songOffset"); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil {
				offset = parsed
			}
		}
		responseSongs := ""
		if offset == 0 {
			responseSongs = strings.Join(searchSongs, "\n    ")
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.8.0">
  <searchResult3>
    %s
  </searchResult3>
</subsonic-response>`, responseSongs)
		_, _ = fmt.Fprint(w, body)
	}
	mux.HandleFunc("/rest/search3", searchHandler)
	mux.HandleFunc("/rest/search3.view", searchHandler)

	getSongHandler := func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		var song string
		switch id {
		case "song1":
			song = searchSongs[0]
		case "song2":
			song = searchSongs[1]
		case "song3":
			song = searchSongs[2]
		default:
			song = `<song id="missing" title="Missing" artist="Unknown" album="Unknown" path="/Music/Missing.mp3" userRating="0" playCount="0" />`
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.8.0">
  %s
</subsonic-response>`, song)
		_, _ = fmt.Fprint(w, body)
	}
	mux.HandleFunc("/rest/getSong", getSongHandler)
	mux.HandleFunc("/rest/getSong.view", getSongHandler)

	playlistsHandler := func(w http.ResponseWriter, _ *http.Request) {
		body := `<?xml version="1.0" encoding="UTF-8"?>
<subsonic-response xmlns="http://subsonic.org/restapi" status="ok" version="1.8.0">
  <playlists></playlists>
</subsonic-response>`
		_, _ = fmt.Fprint(w, body)
	}
	mux.HandleFunc("/rest/getPlaylists", playlistsHandler)
	mux.HandleFunc("/rest/getPlaylists.view", playlistsHandler)
	return httptest.NewServer(mux)
}

func readPlan(t *testing.T, path string) report.SyncPlan {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	var plan report.SyncPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	return plan
}

func readReconcile(t *testing.T, path string) report.ReconcileReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reconcile: %v", err)
	}
	var rec report.ReconcileReport
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal reconcile: %v", err)
	}
	return rec
}

func countTSVRows(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tsv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return 0
	}
	return len(lines) - 1
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}

func setTestFlags(values map[*string]string) func() {
	original := make(map[*string]string, len(values))
	for key, value := range values {
		original[key] = *key
		*key = value
	}
	return func() {
		for key, value := range original {
			*key = value
		}
	}
}

func setTestBoolFlags(values map[*bool]bool) func() {
	original := make(map[*bool]bool, len(values))
	for key, value := range values {
		original[key] = *key
		*key = value
	}
	return func() {
		for key, value := range original {
			*key = value
		}
	}
}
