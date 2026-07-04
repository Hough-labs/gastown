package daemon

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/agentlog"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
)

// contextSessionGracePeriod is how long a newly (re)started persistent agent
// session is exempt from context-pressure checks. A freshly cycled agent's
// Claude Code process needs a moment to write its first usage event; acting
// on a session this young would either read stale data or none at all.
const contextSessionGracePeriod = 10 * time.Minute

// contextBand classifies a context-token snapshot against the configured
// YELLOW/RED thresholds.
type contextBand string

const (
	contextBandNone   contextBand = "none"
	contextBandYellow contextBand = "yellow"
	contextBandRed    contextBand = "red"
)

// classifyContextBand bands a token snapshot against the yellow/red
// thresholds. red is checked first: DaemonThresholds.ContextThresholdsForRoleV
// always clamps red > yellow, so a tokens count crossing red also crosses
// yellow and must report the higher band.
func classifyContextBand(tokens, yellow, red int) contextBand {
	switch {
	case tokens >= red:
		return contextBandRed
	case tokens >= yellow:
		return contextBandYellow
	default:
		return contextBandNone
	}
}

// contextTarget is one persistent-agent session the context watchdog
// monitors.
type contextTarget struct {
	identity    string // restart-tracker key / LifecycleRequest.From (e.g. "deacon", "<rig>-witness")
	role        string // constants.RoleWitness / RoleRefinery / RoleDeacon — selects per-role threshold
	sessionName string
	projectDir  string // Claude Code project dir (per-role CLAUDE_CONFIG_DIR + workDir)
}

// checkContextPressure is the context-lifecycle watchdog (gfork-47p.2):
// heartbeat step 12c. Persistent agents (deacon, witness, refinery) run one
// Claude Code session for 1.5-1.6h with no compaction; left unchecked they
// hit Claude's own context ceiling (RED) and wedge idle-at-prompt.
//
// Each tick, for every monitored role/rig:
//   - YELLOW: nudge the agent to self-cycle at its next loop boundary
//     (`gt handoff --cycle`), cooldown-guarded so a repeatedly-ignored nudge
//     doesn't spam the pane.
//   - RED: force-cycle the session via the daemon's own lifecycle action,
//     but only when the pane is idle (never kill mid-turn) and the restart
//     tracker / usage-limit guards agree it's safe.
func (d *Daemon) checkContextPressure() {
	cfg := d.loadOperationalConfig().GetDaemonConfig()
	now := time.Now()

	for _, target := range d.contextTargets(cfg.ContextCycleRolesV()) {
		d.evaluateContextTarget(target, cfg, now)
	}
}

// contextTargets enumerates the persistent agents monitored by the context
// watchdog: deacon (town-level) plus witness/refinery per operational rig,
// filtered to roles in roles and gated by each role's own patrol toggle.
// Polecats are deliberately never enumerated — their lifecycle belongs to
// reapIdlePolecats / self-terminate (gfork-47p.1), not this recycle path.
func (d *Daemon) contextTargets(roles []string) []contextTarget {
	roleSet := make(map[string]bool, len(roles))
	for _, r := range roles {
		roleSet[r] = true
	}

	var targets []contextTarget
	if roleSet[constants.RoleDeacon] && d.isPatrolActive("deacon") {
		if t, ok := d.buildContextTarget(constants.RoleDeacon, ""); ok {
			targets = append(targets, t)
		}
	}
	if roleSet[constants.RoleWitness] && d.isPatrolActive("witness") {
		for _, rigName := range d.getPatrolRigs("witness") {
			if t, ok := d.buildContextTarget(constants.RoleWitness, rigName); ok {
				targets = append(targets, t)
			}
		}
	}
	if roleSet[constants.RoleRefinery] && d.isPatrolActive("refinery") {
		for _, rigName := range d.getPatrolRigs("refinery") {
			if t, ok := d.buildContextTarget(constants.RoleRefinery, rigName); ok {
				targets = append(targets, t)
			}
		}
	}
	return targets
}

// buildContextTarget resolves the identity, session name, and Claude Code
// project directory for one role/rig pair. Returns ok=false if the identity
// can't be parsed or the working directory / project directory can't be
// determined — skip the target, don't panic (matches the reader-err-skips-
// tick contract used throughout the watchdog).
func (d *Daemon) buildContextTarget(role, rigName string) (contextTarget, bool) {
	var identity string
	switch role {
	case constants.RoleDeacon:
		identity = constants.RoleDeacon
	case constants.RoleWitness:
		identity = rigName + "-witness"
	case constants.RoleRefinery:
		identity = rigName + "-refinery"
	default:
		return contextTarget{}, false
	}

	parsed, err := parseIdentity(identity)
	if err != nil {
		d.logger.Printf("context watchdog: cannot parse identity %q: %v", identity, err)
		return contextTarget{}, false
	}

	workDir := d.getWorkDir(nil, parsed)
	if workDir == "" {
		d.logger.Printf("context watchdog: cannot determine workdir for %s", identity)
		return contextTarget{}, false
	}

	rigPath := ""
	if rigName != "" {
		rigPath = filepath.Join(d.config.TownRoot, rigName)
	}
	configDir := ""
	if rc := config.ResolveRoleAgentConfig(role, d.config.TownRoot, rigPath); rc != nil {
		configDir = rc.Env["CLAUDE_CONFIG_DIR"]
	}

	projectDir, err := agentlog.ProjectDirFor(configDir, workDir)
	if err != nil {
		d.logger.Printf("context watchdog: cannot resolve project dir for %s: %v", identity, err)
		return contextTarget{}, false
	}

	return contextTarget{
		identity:    identity,
		role:        role,
		sessionName: d.identityToSession(identity),
		projectDir:  projectDir,
	}, true
}

// evaluateContextTarget checks one monitored session's context pressure and
// acts on it. Any resolution failure (no session, no JSONL yet, unreadable
// log) skips the target for this tick and logs — it never panics and never
// treats "unknown" as "safe" or "over threshold".
func (d *Daemon) evaluateContextTarget(target contextTarget, cfg *config.DaemonThresholds, now time.Time) {
	hasSession, err := d.tmux.HasSession(target.sessionName)
	if err != nil || !hasSession {
		return
	}

	sessionCreated, err := d.tmux.GetSessionCreatedTime(target.sessionName)
	if err != nil {
		d.logger.Printf("context watchdog: %s: cannot read session start time: %v", target.identity, err)
		return
	}
	if age := now.Sub(sessionCreated); age < contextSessionGracePeriod {
		d.logger.Printf("context watchdog: %s: session %s old, within grace period", target.identity, age.Round(time.Second))
		return
	}

	tokens, err := agentlog.CurrentContextTokens(target.projectDir, sessionCreated)
	if err != nil {
		d.logger.Printf("context watchdog: %s: cannot read context tokens: %v", target.identity, err)
		return
	}

	yellow, red := cfg.ContextThresholdsForRoleV(target.role)
	band := classifyContextBand(tokens, yellow, red)
	d.logger.Printf("context watchdog: %s tokens=%d yellow=%d red=%d band=%s", target.identity, tokens, yellow, red, band)

	switch band {
	case contextBandRed:
		d.tryContextCycle(target)
	case contextBandYellow:
		d.tryContextNudge(target, cfg, tokens, yellow, now)
	}
}

// tryContextCycle force-cycles target's session at RED. Guarded so a
// mid-turn agent is never killed: only acts when the pane is idle, and
// defers to the restart tracker (crash-loop/backoff) and usage-limit
// detector exactly like restartStuckDeacon, so a context-triggered cycle
// can't fight or double-count against those existing safety nets.
func (d *Daemon) tryContextCycle(target contextTarget) {
	if d.restartTracker != nil {
		if d.restartTracker.IsInCrashLoop(target.identity) {
			d.logger.Printf("context watchdog: %s in crash-loop, not cycling", target.identity)
			return
		}
		if !d.restartTracker.CanRestart(target.identity) {
			d.logger.Printf("context watchdog: %s restart in backoff, not cycling", target.identity)
			return
		}
	}

	if !d.tmux.IsIdle(target.sessionName) {
		d.logger.Printf("context watchdog: %s over RED threshold but busy, deferring cycle", target.identity)
		return
	}

	if pane, err := d.tmux.CapturePane(target.sessionName, 30); err == nil && IsClaudeUsageLimit(pane) {
		d.logger.Printf("context watchdog: %s paused on Claude usage-limit, skipping cycle (quota_dog will rotate accounts)", target.identity)
		if d.restartTracker != nil {
			d.restartTracker.RecordPause(target.identity)
			if err := d.restartTracker.Save(); err != nil {
				d.logger.Printf("context watchdog: warning: failed to save restart state: %v", err)
			}
		}
		return
	}

	d.logger.Printf("context watchdog: %s over RED threshold and idle, cycling", target.identity)
	req := &LifecycleRequest{From: target.identity, Action: ActionCycle, Timestamp: time.Now()}
	if err := d.executeLifecycleAction(req); err != nil {
		d.logger.Printf("context watchdog: %s cycle failed: %v", target.identity, err)
		return
	}
	if d.restartTracker != nil {
		d.restartTracker.RecordRestart(target.identity)
		if err := d.restartTracker.Save(); err != nil {
			d.logger.Printf("context watchdog: warning: failed to save restart state: %v", err)
		}
	}
}

// tryContextNudge nudges target at YELLOW to self-cycle at its next loop
// boundary. NudgeSession's input is queued and only processed after the
// agent's current turn completes, so this is boundary-safe by construction —
// a refinery mid-merge finishes before acting on it. Cooldown-guarded via
// contextNudgeLast (heartbeat-goroutine-only map, no lock needed).
func (d *Daemon) tryContextNudge(target contextTarget, cfg *config.DaemonThresholds, tokens, yellow int, now time.Time) {
	cooldown := cfg.ContextNudgeCooldownD()
	if last, ok := d.contextNudgeLast[target.identity]; ok && now.Sub(last) < cooldown {
		d.logger.Printf("context watchdog: %s YELLOW nudge suppressed, within cooldown (%s)", target.identity, cooldown)
		return
	}

	msg := fmt.Sprintf(
		"CONTEXT_YELLOW: session context ~%d tokens (threshold %d). Finish your current step, then run: gt handoff --cycle --reason context-yellow",
		tokens, yellow,
	)
	if err := d.tmux.NudgeSession(target.sessionName, msg); err != nil {
		d.logger.Printf("context watchdog: %s nudge failed: %v", target.identity, err)
		return
	}
	if d.contextNudgeLast == nil {
		d.contextNudgeLast = make(map[string]time.Time)
	}
	d.contextNudgeLast[target.identity] = now
}
