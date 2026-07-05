package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/nudge"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/tmux"
)

// writeHeartbeat writes a synthetic heartbeat file with the given state and
// timestamp into townRoot/.runtime/heartbeats/<session>.json.
func writeHeartbeat(t *testing.T, townRoot, session string, state polecat.HeartbeatState, ts time.Time) {
	t.Helper()
	dir := filepath.Join(townRoot, ".runtime", "heartbeats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir heartbeats: %v", err)
	}
	hb := polecat.SessionHeartbeat{
		Timestamp: ts.UTC(),
		State:     state,
	}
	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, session+".json"), data, 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
}

func TestIsIdleAtPromptWhileStale(t *testing.T) {
	const session = "gt-test-watchdog"
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	staleTS := now.Add(-3 * time.Minute)  // older than idleAtPromptStaleThreshold (2 min)
	freshTS := now.Add(-30 * time.Second) // newer than threshold

	futureBackoff := fmt.Sprintf("backoff-until:%d", now.Add(10*time.Minute).Unix())
	pastBackoff := fmt.Sprintf("backoff-until:%d", now.Add(-5*time.Minute).Unix())

	tests := []struct {
		name   string
		setup  func(townRoot string)
		labels []string
		want   bool
	}{
		{
			name:  "no heartbeat file",
			setup: func(_ string) {},
			want:  false,
		},
		{
			name: "fresh heartbeat idle",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, freshTS)
			},
			want: false,
		},
		{
			name: "stale heartbeat working state",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatWorking, staleTS)
			},
			want: false,
		},
		{
			name: "stale heartbeat exiting state",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatExiting, staleTS)
			},
			want: false,
		},
		{
			name: "stale heartbeat idle no labels",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, staleTS)
			},
			want: true,
		},
		{
			name: "stale heartbeat stuck no labels",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatStuck, staleTS)
			},
			want: true,
		},
		{
			name: "stale heartbeat idle with active backoff",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, staleTS)
			},
			labels: []string{futureBackoff},
			want:   false,
		},
		{
			name: "stale heartbeat idle with expired backoff",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, staleTS)
			},
			labels: []string{pastBackoff},
			want:   true,
		},
		{
			name: "stale heartbeat idle with malformed backoff label",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, staleTS)
			},
			labels: []string{"backoff-until:not-a-number"},
			want:   true, // malformed label is skipped — not treated as active backoff
		},
		{
			name: "stale heartbeat idle unrelated labels",
			setup: func(townRoot string) {
				writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, staleTS)
			},
			labels: []string{"foo", "bar"},
			want:   true,
		},
		{
			name: "stale v1 heartbeat (no state — defaults to working)",
			setup: func(townRoot string) {
				// Write a v1-format heartbeat: timestamp only, no state field.
				dir := filepath.Join(townRoot, ".runtime", "heartbeats")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				data := []byte(`{"timestamp":"` + staleTS.Format(time.RFC3339Nano) + `"}`)
				path := filepath.Join(dir, session+".json")
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			want: false, // v1 defaults to working → not candidate for forced recovery
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			townRoot := t.TempDir()
			tt.setup(townRoot)
			got := IsIdleAtPromptWhileStale(townRoot, session, tt.labels, now)
			if got != tt.want {
				t.Errorf("IsIdleAtPromptWhileStale() = %v; want %v", got, tt.want)
			}
		})
	}
}

// TestForceTurnBoundary_SendsRecoverySequence asserts the recovery action emits
// the proven send-keys sequence (hq-2kaq acceptance criterion #3): cancel any
// partial input (C-c), type the continue directive, and submit it (Enter) — the
// three keystrokes that force a turn boundary so the nudge queue can drain.
func TestForceTurnBoundary_SendsRecoverySequence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows — fake tmux requires bash")
	}
	fakeBinDir := t.TempDir()
	writeFakeContextTmux(t, fakeBinDir)
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	tmuxLog := filepath.Join(t.TempDir(), "tmux.log")
	t.Setenv("TMUX_LOG", tmuxLog)

	const session = "hq-deacon"
	if err := ForceTurnBoundary(tmux.NewTmux(), session, "gt prime --hook"); err != nil {
		t.Fatalf("ForceTurnBoundary: %v", err)
	}

	data, err := os.ReadFile(tmuxLog)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"C-c", "gt prime --hook", "Enter"} {
		if !strings.Contains(got, want) {
			t.Errorf("tmux log missing %q; full log:\n%s", want, got)
		}
	}
}

// TestMaybeForceIdleAtPromptTurnBoundary_FiresThenCooldownSuppresses asserts the
// full watchdog action (hq-2kaq acceptance criterion #3): a stale idle-at-prompt
// session with a pending queued nudge gets a forced turn boundary, and a second
// evaluation within the cooldown window is suppressed — no send-keys storm.
func TestMaybeForceIdleAtPromptTurnBoundary_FiresThenCooldownSuppresses(t *testing.T) {
	townRoot, logBuf, _ := setupContextWatchdogTest(t)
	t.Setenv("FAKE_PANE_MARKER", idlePaneMarker)

	const session = "hq-deacon"
	now := time.Now()

	// Stale + idle heartbeat → IsIdleAtPromptWhileStale reports true.
	writeHeartbeat(t, townRoot, session, polecat.HeartbeatIdle, now.Add(-3*time.Minute))

	// One pending queued nudge that cannot drain without a turn boundary.
	if err := nudge.Enqueue(townRoot, session, nudge.QueuedNudge{
		Sender:  "test",
		Message: "drain me",
	}); err != nil {
		t.Fatalf("enqueue nudge: %v", err)
	}

	d := &Daemon{
		config: &Config{TownRoot: townRoot},
		logger: log.New(logBuf, "", 0),
		tmux:   tmux.NewTmux(),
		// Bogus bd path: getAgentBeadInfo fails fast → no backoff-until labels,
		// which is exactly the genuine-idle path (agent is not in await-signal
		// backoff), so the watchdog is allowed to act.
		bdPath: filepath.Join(t.TempDir(), "no-such-bd"),
	}
	target := contextTarget{identity: "deacon", role: "deacon", sessionName: session}

	// First evaluation: all gates pass → force a turn boundary.
	d.maybeForceIdleAtPromptTurnBoundary(target, now)
	if !strings.Contains(logBuf.String(), "forcing turn boundary") {
		t.Fatalf("expected first evaluation to force a turn boundary, got: %s", logBuf.String())
	}
	if _, ok := d.idleAtPromptForcedLast[session]; !ok {
		t.Fatal("expected idleAtPromptForcedLast to record the forced recovery")
	}

	// Second evaluation one second later: within the 5-minute cooldown → suppressed.
	logBuf.Reset()
	d.maybeForceIdleAtPromptTurnBoundary(target, now.Add(time.Second))
	if !strings.Contains(logBuf.String(), "suppressed (cooldown") {
		t.Errorf("expected cooldown suppression on the second evaluation, got: %s", logBuf.String())
	}
}
