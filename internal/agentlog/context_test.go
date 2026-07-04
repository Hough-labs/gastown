package agentlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectDirFor(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		workDir   string
	}{
		{"explicit config dir", "/home/x/.claude-witness", "/Users/pa/gt/rig"},
		{"empty config dir falls back to $HOME/.claude", "", "/some/work/dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProjectDirFor(tt.configDir, tt.workDir)
			if err != nil {
				t.Fatalf("ProjectDirFor: %v", err)
			}
			configDir := tt.configDir
			if configDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatalf("getting home dir: %v", err)
				}
				configDir = filepath.Join(home, ".claude")
			}
			abs, _ := filepath.Abs(tt.workDir)
			hash := strings.ReplaceAll(filepath.ToSlash(abs), "/", "-")
			want := filepath.Join(configDir, "projects", hash)
			if got != want {
				t.Errorf("ProjectDirFor(%q, %q) = %q, want %q", tt.configDir, tt.workDir, got, want)
			}
		})
	}
}

// ccUsageLine builds one assistant JSONL entry with the given usage fields.
// mtime is not set here — writeJSONLFixture controls file mtime separately.
func ccUsageLine(isSidechain bool, input, output, cacheRead, cacheCreation int) string {
	return fmt.Sprintf(
		`{"type":"assistant","isSidechain":%t,"message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		isSidechain, input, output, cacheRead, cacheCreation,
	)
}

// writeJSONLFixture writes lines (already-serialized JSONL, newline-joined)
// to <dir>/<name>.jsonl and sets its mtime, returning the file path.
func writeJSONLFixture(t *testing.T, dir, name string, lines []string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func TestCurrentContextTokens(t *testing.T) {
	sessionStart := time.Now().Add(-time.Hour)

	t.Run("duplicated usage rows: snapshot not sum", func(t *testing.T) {
		dir := t.TempDir()
		// Same turn's content blocks each write a duplicate usage object;
		// the golden answer is that ONE turn's total, not 3x it.
		lines := []string{
			ccUsageLine(false, 10, 50, 20000, 5000),
			ccUsageLine(false, 10, 50, 20000, 5000),
			ccUsageLine(false, 10, 50, 20000, 5000),
		}
		writeJSONLFixture(t, dir, "session", lines, time.Now())

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		want := 10 + 50 + 20000 + 5000 // one turn's usage, NOT summed across the 3 duplicate lines
		if got != want {
			t.Errorf("got %d, want %d (must be snapshot of newest turn, not sum across duplicate lines)", got, want)
		}
	})

	t.Run("sidechain skip: subagent turn must not mask parent context", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			ccUsageLine(false, 10, 50, 130000, 5000), // parent's real context — the golden answer
			ccUsageLine(true, 5, 20, 2000, 500),      // Task-tool subagent sidechain, appended after
		}
		writeJSONLFixture(t, dir, "session", lines, time.Now())

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		want := 10 + 50 + 130000 + 5000
		if got != want {
			t.Errorf("got %d, want %d (sidechain entry must be skipped)", got, want)
		}
	})

	t.Run("cache-only turn: zero input/output must still count cache tokens", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{
			ccUsageLine(false, 0, 0, 64979, 0), // pure cache-read turn
		}
		writeJSONLFixture(t, dir, "session", lines, time.Now())

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		if got != 64979 {
			t.Errorf("got %d, want %d", got, 64979)
		}
	})

	t.Run("truncated last line: falls back to newest complete line", func(t *testing.T) {
		dir := t.TempDir()
		good := ccUsageLine(false, 10, 50, 40000, 1000)
		lines := []string{
			good,
			`{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tex`, // mid-write truncation
		}
		writeJSONLFixture(t, dir, "session", lines, time.Now())

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		want := 10 + 50 + 40000 + 1000
		if got != want {
			t.Errorf("got %d, want %d (truncated final line must be skipped, not fatal)", got, want)
		}
	})

	t.Run("mtime filtering: stale pre-respawn file excluded", func(t *testing.T) {
		dir := t.TempDir()
		// Old session file, modified BEFORE `since` (the current tmux
		// session's start time) — must be ignored even though it's the only
		// other file present.
		writeJSONLFixture(t, dir, "old-session",
			[]string{ccUsageLine(false, 1, 1, 999999, 999999)},
			sessionStart.Add(-time.Hour))
		// New session file, modified AFTER `since` — this is the one that
		// must be read.
		writeJSONLFixture(t, dir, "new-session",
			[]string{ccUsageLine(false, 10, 50, 30000, 2000)},
			sessionStart.Add(time.Minute))

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		want := 10 + 50 + 30000 + 2000
		if got != want {
			t.Errorf("got %d, want %d (must read newest file after `since`, not the stale one)", got, want)
		}
	})

	t.Run("tail-only: newest usage still found when file exceeds the read window", func(t *testing.T) {
		orig := contextReadWindow
		contextReadWindow = 512 // shrink so a small fixture still exercises the seek path
		t.Cleanup(func() { contextReadWindow = orig })

		dir := t.TempDir()
		var lines []string
		// Pad with filler lines (non-matching type) so the file exceeds the
		// shrunk read window; only the LAST line's usage is the golden answer.
		for i := 0; i < 40; i++ {
			lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"filler line %d filler line %d filler"}]}}`, i, i))
		}
		lines = append(lines, ccUsageLine(false, 10, 50, 45000, 3000))
		writeJSONLFixture(t, dir, "session", lines, time.Now())

		got, err := CurrentContextTokens(dir, sessionStart)
		if err != nil {
			t.Fatalf("CurrentContextTokens: %v", err)
		}
		want := 10 + 50 + 45000 + 3000
		if got != want {
			t.Errorf("got %d, want %d (must find newest usage within the tail read window)", got, want)
		}
	})

	t.Run("empty directory: error, not zero", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := CurrentContextTokens(dir, sessionStart); err == nil {
			t.Error("expected error for directory with no JSONL files, got nil")
		}
	})

	t.Run("no usage event in file: error, not zero", func(t *testing.T) {
		dir := t.TempDir()
		writeJSONLFixture(t, dir, "session",
			[]string{`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`},
			time.Now())
		if _, err := CurrentContextTokens(dir, sessionStart); err == nil {
			t.Error("expected error when no assistant usage event exists, got nil")
		}
	})
}
