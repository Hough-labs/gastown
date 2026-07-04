package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/tmux"
)

func TestClassifyContextBand(t *testing.T) {
	const yellow, red = 1000, 2000
	tests := []struct {
		name   string
		tokens int
		want   contextBand
	}{
		{"well under yellow", 100, contextBandNone},
		{"just under yellow", yellow - 1, contextBandNone},
		{"exactly yellow", yellow, contextBandYellow},
		{"between yellow and red", 1500, contextBandYellow},
		{"just under red", red - 1, contextBandYellow},
		{"exactly red", red, contextBandRed},
		{"well over red", 5000, contextBandRed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyContextBand(tt.tokens, yellow, red)
			if got != tt.want {
				t.Errorf("classifyContextBand(%d, %d, %d) = %s, want %s", tt.tokens, yellow, red, got, tt.want)
			}
		})
	}
}

// TestContextTargets_RoleGating verifies contextTargets() honors the
// configured role set and each role's patrol toggle. Deacon-only: witness
// and refinery enumeration go through getPatrolRigs -> isRigOperational,
// which shells out to `bd` and reads wisp/rig registry state — out of scope
// for this hermetic unit test (covered structurally below instead).
func TestContextTargets_RoleGating(t *testing.T) {
	tests := []struct {
		name            string
		roles           []string
		disabledPatrols map[string]bool
		wantIdentities  []string
	}{
		{
			name:           "deacon in roles, patrol active: one target",
			roles:          []string{constants.RoleDeacon},
			wantIdentities: []string{"deacon"},
		},
		{
			name:            "deacon in roles, patrol disabled: no target",
			roles:           []string{constants.RoleDeacon},
			disabledPatrols: map[string]bool{"deacon": true},
			wantIdentities:  nil,
		},
		{
			name:           "deacon not in configured roles: no target",
			roles:          []string{constants.RoleWitness, constants.RoleRefinery},
			wantIdentities: nil,
		},
		{
			name:           "polecat can never appear: structurally excluded",
			roles:          []string{constants.RoleDeacon, constants.RolePolecat},
			wantIdentities: []string{"deacon"},
		},
		{
			name:           "empty role set: no targets",
			roles:          nil,
			wantIdentities: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			townRoot := t.TempDir()
			d := &Daemon{
				config:          &Config{TownRoot: townRoot},
				logger:          log.New(io.Discard, "", 0),
				disabledPatrols: tt.disabledPatrols,
			}
			targets := d.contextTargets(tt.roles)
			var got []string
			for _, target := range targets {
				got = append(got, target.identity)
			}
			if len(got) != len(tt.wantIdentities) {
				t.Fatalf("contextTargets(%v) identities = %v, want %v", tt.roles, got, tt.wantIdentities)
			}
			for i := range got {
				if got[i] != tt.wantIdentities[i] {
					t.Errorf("contextTargets(%v) identities = %v, want %v", tt.roles, got, tt.wantIdentities)
				}
			}
		})
	}
}

// writeFakeContextTmux writes a fake tmux binary controllable via env vars:
//   - FAKE_HAS_SESSION=1 makes has-session report absent (exit 1); default present.
//   - FAKE_SESSION_CREATED sets the #{session_created} unix timestamp for list-sessions.
//   - FAKE_PANE_MARKER sets the capture-pane content (a nanosecond timestamp
//     comment is appended so consecutive captures never compare equal —
//     otherwise NudgeSession's sendEnterVerified exhausts its full retry
//     budget waiting for pane content to "change").
func writeFakeContextTmux(t *testing.T, dir string) {
	t.Helper()
	script := `#!/usr/bin/env bash
set -euo pipefail

cmd=""
skip_next=0
for arg in "$@"; do
  if [[ "$skip_next" -eq 1 ]]; then
    skip_next=0
    continue
  fi
  if [[ "$arg" == "-u" ]]; then
    continue
  fi
  if [[ "$arg" == "-L" ]]; then
    skip_next=1
    continue
  fi
  cmd="$arg"
  break
done

if [[ -n "${TMUX_LOG:-}" ]]; then
  printf "%s %s\n" "$cmd" "$*" >> "$TMUX_LOG"
fi

if [[ "${1:-}" == "-V" ]]; then
  echo "tmux 3.3a"
  exit 0
fi

if [[ "$cmd" == "has-session" ]]; then
  if [[ "${FAKE_HAS_SESSION:-0}" == "1" ]]; then
    exit 1
  fi
  exit 0
fi

if [[ "$cmd" == "list-sessions" ]]; then
  echo "${FAKE_SESSION_CREATED:-0}"
  exit 0
fi

if [[ "$cmd" == "capture-pane" ]]; then
  printf '%s\n# ts %s\n' "${FAKE_PANE_MARKER:-}" "$(date +%s%N)"
  exit 0
fi

exit 0
`
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
}

// idlePaneMarker is capture-pane content IsIdle reports as idle (ready
// prompt, no busy indicator).
const idlePaneMarker = "❯ "

// busyPaneMarker is capture-pane content IsIdle reports as busy.
const busyPaneMarker = "Thinking… (esc to interrupt)"

// usageLimitPaneMarker is idle (ready prompt present) AND matches
// IsClaudeUsageLimit, for testing the RED-tier usage-limit guard.
const usageLimitPaneMarker = idlePaneMarker + "\nClaude usage limit reached, resets 2pm"

func setupContextWatchdogTest(t *testing.T) (townRoot string, logBuf *strings.Builder, teardown func()) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows — fake tmux requires bash")
	}
	townRoot = t.TempDir()
	fakeBinDir := t.TempDir()
	writeFakeContextTmux(t, fakeBinDir)
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	logBuf = &strings.Builder{}
	return townRoot, logBuf, func() {}
}

// fakeUsageLine builds one minimal assistant JSONL line with the given
// snapshot token count (all in input_tokens, for a simple single-field golden
// number — CurrentContextTokens itself is already covered exhaustively in
// internal/agentlog).
func fakeUsageLine(tokens int) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":%d,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, tokens)
}

func writeContextFixture(t *testing.T, projectDir string, tokens int, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir projectDir: %v", err)
	}
	path := filepath.Join(projectDir, "session.jsonl")
	if err := os.WriteFile(path, []byte(fakeUsageLine(tokens)+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestEvaluateContextTarget(t *testing.T) {
	// Yellow/red are chosen above contextYellowTokensFloor (50k) so the
	// clamp in ContextThresholdsForRoleV doesn't silently override them.
	testThresholds := &config.DaemonThresholds{
		ContextYellowTokens: intPtr(60_000),
		ContextRedTokens:    intPtr(120_000),
	}
	sessionOld := time.Now().Add(-time.Hour)

	t.Run("no session: returns silently, no panic", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_HAS_SESSION", "1")
		d := &Daemon{config: &Config{TownRoot: townRoot}, logger: log.New(logBuf, "", 0), tmux: tmux.NewTmux()}

		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: t.TempDir()}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		if logBuf.Len() != 0 {
			t.Errorf("expected no log output when session absent, got: %s", logBuf.String())
		}
	})

	t.Run("grace period: young session skipped before reading tokens", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", time.Now().Unix()))
		d := &Daemon{config: &Config{TownRoot: townRoot}, logger: log.New(logBuf, "", 0), tmux: tmux.NewTmux()}

		// No JSONL fixture at all — if the grace-period gate didn't fire
		// first, CurrentContextTokens would error and we'd see that log line
		// instead.
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: t.TempDir()}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "within grace period") {
			t.Errorf("expected grace-period log, got: %s", out)
		}
		if strings.Contains(out, "cannot read context tokens") {
			t.Errorf("must not attempt to read tokens during grace period, got: %s", out)
		}
	})

	t.Run("reader error: no JSONL file skips the tick, does not panic", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		d := &Daemon{config: &Config{TownRoot: townRoot}, logger: log.New(logBuf, "", 0), tmux: tmux.NewTmux()}

		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: t.TempDir()}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "cannot read context tokens") {
			t.Errorf("expected reader-error log, got: %s", out)
		}
	})

	t.Run("band none: below yellow takes no action", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		d := &Daemon{config: &Config{TownRoot: townRoot}, logger: log.New(logBuf, "", 0), tmux: tmux.NewTmux()}

		projectDir := t.TempDir()
		writeContextFixture(t, projectDir, 500, time.Now())
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: projectDir}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "band=none") {
			t.Errorf("expected band=none, got: %s", out)
		}
		if strings.Contains(out, "cycling") || strings.Contains(out, "nudging") {
			t.Errorf("must not act below YELLOW, got: %s", out)
		}
	})

	t.Run("band red: busy pane defers the cycle", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		t.Setenv("FAKE_PANE_MARKER", busyPaneMarker)
		d := &Daemon{
			config:         &Config{TownRoot: townRoot},
			logger:         log.New(logBuf, "", 0),
			tmux:           tmux.NewTmux(),
			restartTracker: NewRestartTracker(townRoot, DefaultRestartTrackerConfig()),
		}

		projectDir := t.TempDir()
		writeContextFixture(t, projectDir, 150_000, time.Now())
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: projectDir}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "over RED threshold but busy, deferring cycle") {
			t.Errorf("expected busy-defer log, got: %s", out)
		}
		if strings.Contains(out, "over RED threshold and idle, cycling") {
			t.Errorf("must not attempt cycle while busy, got: %s", out)
		}
	})

	t.Run("band red: crash-loop guard blocks the cycle", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		t.Setenv("FAKE_PANE_MARKER", idlePaneMarker)
		rt := NewRestartTracker(townRoot, DefaultRestartTrackerConfig())
		// Force a crash-loop state directly (same-package access to unexported state).
		rt.state.Agents["deacon"] = &AgentRestartInfo{CrashLoopSince: time.Now()}
		d := &Daemon{
			config:         &Config{TownRoot: townRoot},
			logger:         log.New(logBuf, "", 0),
			tmux:           tmux.NewTmux(),
			restartTracker: rt,
		}

		projectDir := t.TempDir()
		writeContextFixture(t, projectDir, 150_000, time.Now())
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: projectDir}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "in crash-loop, not cycling") {
			t.Errorf("expected crash-loop guard log, got: %s", out)
		}
	})

	t.Run("band red: backoff guard blocks the cycle", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		t.Setenv("FAKE_PANE_MARKER", idlePaneMarker)
		rt := NewRestartTracker(townRoot, DefaultRestartTrackerConfig())
		rt.RecordRestart("deacon") // sets BackoffUntil in the future (InitialBackoff default 30s)
		d := &Daemon{
			config:         &Config{TownRoot: townRoot},
			logger:         log.New(logBuf, "", 0),
			tmux:           tmux.NewTmux(),
			restartTracker: rt,
		}

		projectDir := t.TempDir()
		writeContextFixture(t, projectDir, 150_000, time.Now())
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: projectDir}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "restart in backoff, not cycling") {
			t.Errorf("expected backoff guard log, got: %s", out)
		}
	})

	t.Run("band red: usage-limit guard pauses instead of cycling", func(t *testing.T) {
		townRoot, logBuf, _ := setupContextWatchdogTest(t)
		t.Setenv("FAKE_SESSION_CREATED", fmt.Sprintf("%d", sessionOld.Unix()))
		t.Setenv("FAKE_PANE_MARKER", usageLimitPaneMarker)
		rt := NewRestartTracker(townRoot, DefaultRestartTrackerConfig())
		d := &Daemon{
			config:         &Config{TownRoot: townRoot},
			logger:         log.New(logBuf, "", 0),
			tmux:           tmux.NewTmux(),
			restartTracker: rt,
		}

		projectDir := t.TempDir()
		writeContextFixture(t, projectDir, 150_000, time.Now())
		target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon", projectDir: projectDir}
		d.evaluateContextTarget(target, testThresholds, time.Now())

		out := logBuf.String()
		if !strings.Contains(out, "paused on Claude usage-limit") {
			t.Errorf("expected usage-limit guard log, got: %s", out)
		}
		if rt.CanRestart("deacon") {
			t.Error("expected RecordPause to apply a backoff window")
		}
	})

	// Deliberately NOT tested here: RED band + idle + all guards clear, all
	// the way through executeLifecycleAction's actual respawn. Verified by
	// hand that this hangs for minutes against a faked tmux binary — the
	// pre-existing restart/session-readiness machinery it drives (readiness
	// polling, dialog detection) assumes a real Claude process on the other
	// end and is out of scope for this bead. The guard tests above cover
	// every branch that decides whether that call is reached; the "over RED
	// threshold and idle, cycling" log line firing (or not) is exercised
	// implicitly via the busy/crash-loop/backoff/usage-limit tests, which
	// all assert its ABSENCE.
}

func TestTryContextNudge(t *testing.T) {
	cfg := &config.DaemonThresholds{ContextNudgeCooldown: "15m"}

	townRoot, logBuf, _ := setupContextWatchdogTest(t)
	t.Setenv("FAKE_PANE_MARKER", idlePaneMarker)
	d := &Daemon{
		config: &Config{TownRoot: townRoot},
		logger: log.New(logBuf, "", 0),
		tmux:   tmux.NewTmux(),
	}

	target := contextTarget{identity: "deacon", role: "deacon", sessionName: "hq-deacon"}
	now := time.Now()

	d.tryContextNudge(target, cfg, 1500, 1000, now)
	if _, ok := d.contextNudgeLast["deacon"]; !ok {
		t.Fatal("expected contextNudgeLast to be recorded after a nudge")
	}
	firstLog := logBuf.String()
	if strings.Contains(firstLog, "nudge suppressed") {
		t.Errorf("first nudge must not be suppressed, got: %s", firstLog)
	}

	// Second nudge, immediately after, well within the 15m cooldown: must be
	// suppressed WITHOUT calling through to tmux again (fast path — no real
	// NudgeSession round-trip).
	logBuf.Reset()
	d.tryContextNudge(target, cfg, 1600, 1000, now.Add(time.Second))
	secondLog := logBuf.String()
	if !strings.Contains(secondLog, "nudge suppressed") {
		t.Errorf("expected cooldown suppression, got: %s", secondLog)
	}
}

func intPtr(v int) *int { return &v }
