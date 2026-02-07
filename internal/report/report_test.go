package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTSVUsesTabs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.tsv")
	if err := WriteTSV(path, []string{"col1", "col2"}, [][]string{{"a\tb", "c"}}); err != nil {
		t.Fatalf("failed to write tsv: %s", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read tsv: %s", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and row, got %d line(s)", len(lines))
	}
	if lines[0] != "col1\tcol2" {
		t.Fatalf("expected tab-delimited header, got %q", lines[0])
	}
	if lines[1] != "a b\tc" {
		t.Fatalf("expected tab-delimited row with sanitized tab, got %q", lines[1])
	}
}
