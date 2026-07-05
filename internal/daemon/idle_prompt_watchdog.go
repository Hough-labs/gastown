package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/nudge"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/tmux"
)

// idleAtPromptStaleThreshold is the minimum heartbeat age before the watchdog
// considers a session "no recent turn boundary." Set well below the 17-min
// recurrence and well above the normal brief inter-turn pause.
const idleAtPromptStaleThreshold = 2 * time.Minute

// idleAtPromptForceCooldown prevents send-keys storms: a second force-turn-boundary
// on the same session is suppressed until this window elapses.
const idleAtPromptForceCooldown = 5 * time.Minute

// IsIdleAtPromptWhileStale returns true when a session is a candidate for
// forced turn-boundary recovery (hq-2kaq):
//
//   - heartbeat is stale (> idleAtPromptStaleThreshold old) — no recent gt
//     command = no recent turn boundary
//   - heartbeat state is idle or unset — not actively working or exiting
//   - no active await-signal backoff (backoff-until label absent or expired)
//
// Returns false when:
//   - no heartbeat file exists (unknown state — don't false-positive)
//   - heartbeat is fresh (< threshold old)
//   - heartbeat state is working or exiting
//   - agentBeadLabels contains a backoff-until:TIMESTAMP in the future
func IsIdleAtPromptWhileStale(townRoot, sessionName string, agentBeadLabels []string, now time.Time) bool {
	hb := polecat.ReadSessionHeartbeat(townRoot, sessionName)
	if hb == nil {
		return false // no heartbeat = no basis for classification
	}

	if now.Sub(hb.Timestamp) < idleAtPromptStaleThreshold {
		return false // not yet stale
	}

	state := hb.EffectiveState()
	if state == polecat.HeartbeatWorking || state == polecat.HeartbeatExiting {
		return false // agent is active or in gt-done flow
	}

	// Don't interrupt a legitimate await-signal backoff window.
	// The deacon sets "backoff-until:UNIX_TIMESTAMP" on its agent bead
	// when gt mol step await-signal computes an exponential backoff interval.
	for _, label := range agentBeadLabels {
		if !strings.HasPrefix(label, "backoff-until:") {
			continue
		}
		tsStr := label[len("backoff-until:"):]
		var ts int64
		if _, err := fmt.Sscanf(tsStr, "%d", &ts); err != nil {
			continue
		}
		if time.Unix(ts, 0).After(now) {
			return false // active backoff window — leave the agent alone
		}
	}

	return true
}

// ForceTurnBoundary sends the proven recovery key sequence to an idle-at-prompt
// session so any queued nudges can drain at the next turn boundary (hq-2kaq):
//
//  1. C-c — cancel any pending partial input or escape sequence
//  2. C-u via SendKeysReplace — clear the input line
//  3. continueCmd — literal text of the continue directive (e.g. "gt prime --hook")
//  4. Enter — submit the command, creating the turn boundary
//
// The caller is responsible for cooldown enforcement.
func ForceTurnBoundary(t *tmux.Tmux, session, continueCmd string) error {
	if err := t.SendKeysRaw(session, "C-c"); err != nil {
		return fmt.Errorf("idle-at-prompt recovery: C-c failed: %w", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := t.SendKeysReplace(session, continueCmd, 100); err != nil {
		return fmt.Errorf("idle-at-prompt recovery: continue directive failed: %w", err)
	}
	return nil
}

// checkIdleAtPromptSessions detects patrol agents (deacon, witness, refinery)
// stuck idle-at-prompt with pending queued nudges and forces a turn boundary.
//
// This is the short-threshold (2-3 min) complement to the coarse 30-min
// HungSessionThreshold: it catches the ~17-min recurrence where a loop step
// completes but the agent lands at an interactive prompt instead of chaining
// to await-signal, leaving queued nudges unable to drain (hq-2kaq).
//
// A session is acted on when ALL of the following hold:
//  1. tmux session is alive
//  2. IsIdleAtPromptWhileStale reports true (stale heartbeat, not in backoff)
//  3. nudge queue has ≥1 pending message
//  4. tmux.IsIdle confirms the agent is at the prompt right now (not mid-turn)
//  5. cooldown window has elapsed since the last forced recovery
func (d *Daemon) checkIdleAtPromptSessions() {
	if !d.tmux.IsAvailable() {
		return
	}
	now := time.Now()

	cfg := d.loadOperationalConfig().GetDaemonConfig()
	targets := d.contextTargets(cfg.ContextCycleRolesV())
	for _, target := range targets {
		d.maybeForceIdleAtPromptTurnBoundary(target, now)
	}
}

// maybeForceIdleAtPromptTurnBoundary evaluates one session and forces a turn
// boundary if all idle-at-prompt-while-stale conditions are met.
func (d *Daemon) maybeForceIdleAtPromptTurnBoundary(target contextTarget, now time.Time) {
	// 1. Alive?
	alive, err := d.tmux.HasSession(target.sessionName)
	if err != nil || !alive {
		return
	}

	// 2. Fetch agent bead labels for backoff-until check.
	var labels []string
	agentBeadID := d.identityToAgentBeadID(target.identity)
	if agentBeadID != "" {
		if info, err := d.getAgentBeadInfo(agentBeadID); err == nil {
			labels = info.Labels
		}
	}

	// 3. Is the session idle-at-prompt-while-stale?
	if !IsIdleAtPromptWhileStale(d.config.TownRoot, target.sessionName, labels, now) {
		return
	}

	// 4. Pending nudges? (cheap filesystem check — only act if there's something to drain)
	pending := nudge.QueueLen(d.config.TownRoot, target.sessionName)
	if pending == 0 {
		return
	}

	// 5. Is the pane actually at the prompt right now? (confirms no race with
	//    a turn that just started; avoids injecting mid-turn)
	if !d.tmux.IsIdle(target.sessionName) {
		return
	}

	// 6. Cooldown: one forced recovery per cooldown window per session.
	if d.idleAtPromptForcedLast == nil {
		d.idleAtPromptForcedLast = make(map[string]time.Time)
	}
	if last, ok := d.idleAtPromptForcedLast[target.sessionName]; ok && now.Sub(last) < idleAtPromptForceCooldown {
		d.logger.Printf("idle-at-prompt watchdog: %s: suppressed (cooldown %s remaining)",
			target.sessionName, (idleAtPromptForceCooldown - now.Sub(last)).Round(time.Second))
		return
	}

	d.logger.Printf("idle-at-prompt watchdog: %s idle+stale with %d pending nudge(s), forcing turn boundary",
		target.sessionName, pending)

	continueCmd := "gt prime --hook"
	if err := ForceTurnBoundary(d.tmux, target.sessionName, continueCmd); err != nil {
		d.logger.Printf("idle-at-prompt watchdog: %s: force failed: %v", target.sessionName, err)
		return
	}
	d.idleAtPromptForcedLast[target.sessionName] = now
}
