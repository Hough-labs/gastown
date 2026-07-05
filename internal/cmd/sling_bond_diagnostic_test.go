package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBondFailureDiagnostic verifies the diagnostic string captures the context
// needed to root-cause a stale-read / wrong-database bond failure (gfork-i3d):
// working directory, resolved .beads database, exact bd argv, and bd stderr.
func TestBondFailureDiagnostic(t *testing.T) {
	workDir := t.TempDir()
	args := []string{"mol", "bond", "gt-wisp-1", "gt-abc", "--json"}

	got := bondFailureDiagnostic(workDir, args, "  'gt-abc' not found (not an issue ID or formula name)\n")
	for _, want := range []string{
		"cwd=" + workDir,
		"beads_dir=",
		"cmd=[bd mol bond gt-wisp-1 gt-abc --json]",
		`bd_stderr="'gt-abc' not found`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic missing %q; got: %s", want, got)
		}
	}

	// Blank stderr must not add an empty bd_stderr= segment.
	if got := bondFailureDiagnostic(workDir, args, "   \n"); strings.Contains(got, "bd_stderr=") {
		t.Errorf("expected no bd_stderr segment for blank stderr; got: %s", got)
	}
}

// TestInstantiateFormulaOnBead_BondFailureSurfacesDiagnostic drives the full
// helper with a stub bd whose `mol bond` fails (exit 1 + stderr), and asserts
// the returned error surfaces the enriched diagnostic instead of a bare
// "exit status 1" — the whole point of the gfork-i3d instrumentation.
func TestInstantiateFormulaOnBead_BondFailureSurfacesDiagnostic(t *testing.T) {
	townRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(townRoot, "mayor", "rig"), 0o755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	rigDir := filepath.Join(townRoot, "gastown", "mayor", "rig")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatalf("mkdir rigDir: %v", err)
	}
	routes := strings.Join([]string{
		`{"prefix":"gt-","path":"gastown/mayor/rig"}`,
		`{"prefix":"hq-","path":"."}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(routes), 0o644); err != nil {
		t.Fatalf("write routes.jsonl: %v", err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}

	// Stub bd: cook/wisp succeed; `mol bond` fails with a "not found" on stderr
	// (both the primary wisp->bead bond and the direct-formula fallback hit this).
	bdScript := `#!/bin/sh
echo "CMD:$*" >> "${BD_LOG}"
cmd="$1"
shift || true
case "$cmd" in
  cook) ;;
  mol)
    sub="$1"
    shift || true
    case "$sub" in
      wisp) echo '{"new_epic_id":"gt-wisp-288"}' ;;
      bond)
        echo "'gt-abc123' not found (not an issue ID or formula name)" >&2
        exit 1
        ;;
    esac
    ;;
esac
exit 0
`
	bdScriptWindows := `@echo off
echo CMD:%*>>"%BD_LOG%"
set "cmd=%1"
set "sub=%2"
if "%cmd%"=="cook" exit /b 0
if "%cmd%"=="mol" (
  if "%sub%"=="wisp" (
    echo {^"new_epic_id^":^"gt-wisp-288^"}
    exit /b 0
  )
  if "%sub%"=="bond" (
    echo 'gt-abc123' not found ^(not an issue ID or formula name^) 1>&2
    exit /b 1
  )
)
exit /b 0
`
	_ = writeBDStub(t, binDir, bdScript, bdScriptWindows)

	t.Setenv("BD_LOG", filepath.Join(townRoot, "bd.log"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(filepath.Join(townRoot, "mayor", "rig")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err = InstantiateFormulaOnBead(context.Background(), "mol-polecat-work", "gt-abc123", "Test Bug Fix", "", townRoot, false, nil)
	if err == nil {
		t.Fatal("expected InstantiateFormulaOnBead to fail when bond fails")
	}
	msg := err.Error()
	for _, want := range []string{"beads_dir=", "cmd=[bd mol bond", "bd_stderr=", "not found"} {
		if !strings.Contains(msg, want) {
			t.Errorf("bond failure error missing diagnostic %q; got: %s", want, msg)
		}
	}
}
