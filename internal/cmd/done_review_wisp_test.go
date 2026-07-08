package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// writeWispBdStub installs a shell `bd` stub on PATH that returns two open step
// children for the wisp and logs every close target (bead IDs, skipping flags)
// to a returned log path. Mirrors the harness in done_closeDescendants_test.go.
func writeWispBdStub(t *testing.T, wispID string) (bd *beads.Beads, closesLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script bd stub not supported on Windows")
	}

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	closesLog = filepath.Join(root, "closes.log")

	// Skip any leading global flags, dispatch on the subcommand. `list` for the
	// wisp returns two open children (grandchildren lists return empty), `close`
	// logs the non-flag positional args.
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in --*) shift ;; *) break ;; esac
done
cmd="$1"; shift || true
case "$cmd" in
  list)
    if echo "$*" | grep -q "parent=` + wispID + `"; then
      echo '[{"id":"gt-step-1","title":"Step 1","status":"open"},{"id":"gt-step-2","title":"Step 2","status":"open"}]'
    else
      echo '[]'
    fi
    ;;
  close)
    for arg in "$@"; do
      case "$arg" in --*) continue ;; esac
      echo "$arg" >> "` + closesLog + `"
    done
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return beads.New(root), closesLog
}

// TestCloseAttachedWisp_ClosesWispAndDescendants guards the gfork-4sz fix: a
// review-only reviewer's gt done must leave nothing dangling. closeAttachedWisp
// closes the wisp's step descendants first, then force-closes the wisp root.
// It is wired into both the no-MR close path (where the base bead is
// force-closed ahead of updateAgentStateOnDone's now-skipped wisp close) and
// updateAgentStateOnDone itself.
func TestCloseAttachedWisp_ClosesWispAndDescendants(t *testing.T) {
	bd, closesLog := writeWispBdStub(t, "gt-wisp-xyz")

	issue := &beads.Issue{
		ID: "gt-base-123",
		Description: strings.Join([]string{
			"attached_formula: mol-polecat-review-pr",
			"attached_molecule: gt-wisp-xyz",
			"review_only: true",
		}, "\n"),
	}

	if err := closeAttachedWisp(bd, issue); err != nil {
		t.Fatalf("closeAttachedWisp: %v", err)
	}

	data, err := os.ReadFile(closesLog)
	if err != nil {
		t.Fatalf("no beads were closed (wisp orphaned): %v", err)
	}
	closes := string(data)
	for _, want := range []string{"gt-step-1", "gt-step-2", "gt-wisp-xyz"} {
		if !strings.Contains(closes, want) {
			t.Errorf("expected %s to be closed, close log:\n%s", want, closes)
		}
	}
}

// TestCloseAttachedWisp_NoMolecule verifies the helper is a safe no-op when the
// bead has no attached molecule — no bd close is issued and no error returned.
func TestCloseAttachedWisp_NoMolecule(t *testing.T) {
	bd, closesLog := writeWispBdStub(t, "gt-wisp-xyz")

	issue := &beads.Issue{
		ID:          "gt-base-123",
		Description: "review_only: true", // attachment fields present, but no molecule
	}

	if err := closeAttachedWisp(bd, issue); err != nil {
		t.Fatalf("closeAttachedWisp (no molecule): %v", err)
	}

	if _, err := os.Stat(closesLog); err == nil {
		data, _ := os.ReadFile(closesLog)
		t.Errorf("expected no closes for a bead without an attached molecule, got:\n%s", data)
	}
}
