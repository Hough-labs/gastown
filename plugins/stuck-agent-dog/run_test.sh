#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/plugins/stuck-agent-dog/run.sh"
ORIGINAL_PATH="$PATH"
PASS=0
FAIL=0
CLEANUP_DIRS=()

cleanup() {
  for dir in "${CLEANUP_DIRS[@]}"; do
    rm -rf "$dir"
  done
}
trap cleanup EXIT

record_pass() {
  PASS=$((PASS + 1))
  printf 'PASS: %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  printf 'FAIL: %s\n' "$1"
}

assert_file_empty() {
  local file="$1"
  local label="$2"
  if [ ! -s "$file" ]; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  unexpected contents of %s:\n' "$file"
    sed 's/^/    /' "$file"
  fi
}

assert_file_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  if grep -Fq "$needle" "$file"; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  expected %q in %s\n' "$needle" "$file"
    sed 's/^/    /' "$file" 2>/dev/null || true
  fi
}

assert_line_count() {
  local file="$1"
  local expected="$2"
  local label="$3"
  local actual=0

  if [ -f "$file" ]; then
    actual=$(wc -l < "$file" | tr -d ' ')
  fi
  if [ "$actual" = "$expected" ]; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  expected %s lines in %s, got %s\n' "$expected" "$file" "$actual"
    sed 's/^/    /' "$file" 2>/dev/null || true
  fi
}

write_fake_commands() {
  local bin_dir="$1"

  cat > "$bin_dir/gt" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  town)
    if [ "${2:-}" = "root" ]; then
      printf '%s\n' "$GT_TOWN_ROOT"
      exit 0
    fi
    ;;
  hook)
    if [ "${2:-}" = "show" ]; then
      target="${3:-}"
      name="${target##*/}"
      if [ -f "$TEST_STATE/nohook/$name" ]; then
        printf '{"bead_id":""}\n'
      else
        printf '{"bead_id":"gt-hook-%s"}\n' "$name"
      fi
      exit 0
    fi
    ;;
  polecat)
    if [ "${2:-}" = "check-recovery" ]; then
      target="${3:-}"
      name="${target##*/}"
      # Default to NEEDS_RECOVERY with no active_mr so a polecat without explicit
      # state keeps the pre-existing hook-based restart behavior.
      verdict="NEEDS_RECOVERY"
      if [ -f "$TEST_STATE/verdict/$name" ]; then
        verdict=$(tr -d '\n' < "$TEST_STATE/verdict/$name")
      fi
      active_mr=""
      if [ -f "$TEST_STATE/active_mr/$name" ]; then
        active_mr=$(tr -d '\n' < "$TEST_STATE/active_mr/$name")
      fi
      printf '{"verdict":"%s","active_mr":"%s"}\n' "$verdict" "$active_mr"
      exit 0
    fi
    ;;
  session)
    if [ "${2:-}" = "health" ]; then
      session="${3:-}"
      shift 3
      max_inactivity="0s"
      while [ "$#" -gt 0 ]; do
        case "$1" in
          --max-inactivity)
            max_inactivity="${2:-}"
            shift 2
            ;;
          *)
            shift
            ;;
        esac
      done

      status="healthy"
      if [ -f "$TEST_STATE/health/$session" ]; then
        status=$(tr -d '\n' < "$TEST_STATE/health/$session")
      fi
      # Re-check simulation: after the first probe of a session, switch to its
      # "health_after" status if one is set. Models an agent that recovers
      # between the enumeration scan and the pre-escalation live re-check.
      prior=$(grep -c "^$session --" "$TEST_STATE/health_calls.log" 2>/dev/null || true)
      if [ "${prior:-0}" -ge 1 ] && [ -f "$TEST_STATE/health_after/$session" ]; then
        status=$(tr -d '\n' < "$TEST_STATE/health_after/$session")
      fi
      printf '%s --max-inactivity %s\n' "$session" "$max_inactivity" >> "$TEST_STATE/health_calls.log"
      healthy=false
      zombie=false
      case "$status" in
        healthy) healthy=true ;;
        agent-dead|agent-hung) zombie=true ;;
      esac
      printf '{"session":"%s","status":"%s","healthy":%s,"zombie":%s,"max_inactivity_seconds":0}\n' "$session" "$status" "$healthy" "$zombie"
      exit 0
    fi
    ;;
  mail)
    if [ "${2:-}" = "send" ]; then
      printf '%s\n' "$*" >> "$TEST_STATE/mail.log"
      while IFS= read -r _line; do :; done
      exit 0
    fi
    ;;
  escalate)
    printf '%s\n' "$*" >> "$TEST_STATE/escalate.log"
    exit 0
    ;;
esac

printf 'unexpected gt call: %s\n' "$*" >&2
exit 1
SH
  chmod +x "$bin_dir/gt"

  cat > "$bin_dir/tmux" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

arg_after_t() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "-t" ]; then
      printf '%s\n' "${2:-}"
      return 0
    fi
    shift
  done
  return 1
}

case "${1:-}" in
  has-session)
    session=$(arg_after_t "$@" || true)
    [ -n "$session" ] && [ -f "$TEST_STATE/sessions/$session" ]
    ;;
  kill-session)
    session=$(arg_after_t "$@" || true)
    printf '%s\n' "$session" >> "$TEST_STATE/kill.log"
    ;;
  list-panes)
    printf '999\n'
    ;;
  display-message)
    date +%s
    ;;
  capture-pane)
    printf 'active opencode research in progress\n'
    ;;
  *)
    printf 'unexpected tmux call: %s\n' "$*" >&2
    exit 1
    ;;
esac
SH
  chmod +x "$bin_dir/tmux"

  cat > "$bin_dir/bd" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  show)
    bead="${2:-}"
    status="open"
    if [ -f "$TEST_STATE/status/$bead" ]; then
      status=$(tr -d '\n' < "$TEST_STATE/status/$bead")
    fi
    printf '[{"status":"%s"}]\n' "$status"
    ;;
  list)
    printf '[]\n'
    ;;
  create)
    printf '%s\n' "$*" >> "$TEST_STATE/bd.log"
    ;;
  *)
    printf 'unexpected bd call: %s\n' "$*" >&2
    exit 1
    ;;
esac
SH
  chmod +x "$bin_dir/bd"

  cat > "$bin_dir/ps" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

if [ "${1:-}" = "-o" ] && [ "${2:-}" = "comm=" ]; then
  printf 'bash\n'
  exit 0
fi

printf 'unexpected ps call: %s\n' "$*" >&2
exit 1
SH
  chmod +x "$bin_dir/ps"
}

setup_case() {
  TEST_TMP=$(mktemp -d)
  CLEANUP_DIRS+=("$TEST_TMP")
  export TEST_STATE="$TEST_TMP/state"
  export GT_TOWN_ROOT="$TEST_TMP/town"
  local bin_dir="$TEST_TMP/bin"

  mkdir -p "$TEST_STATE/health" "$TEST_STATE/health_after" "$TEST_STATE/nohook" "$TEST_STATE/sessions" "$TEST_STATE/status" "$TEST_STATE/verdict" "$TEST_STATE/active_mr" "$bin_dir"
  mkdir -p "$GT_TOWN_ROOT/gastown/polecats" "$GT_TOWN_ROOT/deacon"
  printf '{"rigs":{"gastown":{"beads":{"prefix":"gt"}}}}\n' > "$GT_TOWN_ROOT/rigs.json"
  : > "$TEST_STATE/mail.log"
  : > "$TEST_STATE/kill.log"
  : > "$TEST_STATE/escalate.log"
  : > "$TEST_STATE/health_calls.log"
  : > "$TEST_STATE/bd.log"
  touch "$TEST_STATE/sessions/hq-deacon"

  write_fake_commands "$bin_dir"
  export PATH="$bin_dir:$ORIGINAL_PATH"
  export GT_STUCK_AGENT_DOG_MAX_INACTIVITY=0s
  unset GT_STUCK_AGENT_DOG_MASS_DEATH_THRESHOLD
}

add_polecat() {
  local name="$1"
  local status="$2"
  local session="gt-$name"

  mkdir -p "$GT_TOWN_ROOT/gastown/polecats/$name"
  touch "$TEST_STATE/sessions/$session"
  printf '%s\n' "$status" > "$TEST_STATE/health/$session"
}

run_script() {
  bash "$SCRIPT" > "$TEST_STATE/output.log" 2>&1
}

test_healthy_runtime() {
  local runtime="$1"

  setup_case
  add_polecat "$runtime" healthy
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "$runtime healthy: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "$runtime healthy: no restart mail"
  assert_file_empty "$TEST_STATE/escalate.log" "$runtime healthy: no escalation"
  assert_file_contains "$TEST_STATE/health_calls.log" "gt-$runtime --max-inactivity 0s" "$runtime healthy: used central health"
}

test_long_research_active_pane() {
  setup_case
  export GT_STUCK_AGENT_DOG_MAX_INACTIVITY=30m
  add_polecat research agent-hung
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "active research: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "active research: no restart mail"
  assert_file_empty "$TEST_STATE/escalate.log" "active research: no mass-death escalation"
  assert_file_contains "$TEST_STATE/output.log" "OBSERVE: gt-research runtime alive" "active research: observed live runtime"
  assert_file_contains "$TEST_STATE/output.log" "0 crashed, 0 stuck, 1 healthy" "active research: counted healthy"
}

test_dead_agent_restarts_one() {
  setup_case
  add_polecat alpha agent-dead
  run_script

  assert_line_count "$TEST_STATE/kill.log" 1 "dead agent: one session kill"
  assert_file_contains "$TEST_STATE/kill.log" "gt-alpha" "dead agent: killed target session"
  assert_line_count "$TEST_STATE/mail.log" 1 "dead agent: one restart mail"
  assert_file_contains "$TEST_STATE/mail.log" "gastown/witness" "dead agent: mailed rig witness"
  assert_file_empty "$TEST_STATE/escalate.log" "dead agent: no mass-death escalation"
}

test_dead_session_restarts_one() {
  setup_case
  add_polecat beta session-dead
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "dead session: no session kill"
  assert_line_count "$TEST_STATE/mail.log" 1 "dead session: one restart mail"
  assert_file_contains "$TEST_STATE/mail.log" "RESTART_POLECAT: gastown/beta" "dead session: restart requested"
  assert_file_empty "$TEST_STATE/escalate.log" "dead session: no mass-death escalation"
}

test_closed_hook_skips_restart() {
  setup_case
  add_polecat alpha agent-dead
  printf 'closed\n' > "$TEST_STATE/status/gt-hook-alpha"
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "closed hook: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "closed hook: no restart mail"
  assert_file_contains "$TEST_STATE/output.log" "bead closed" "closed hook: status checked"
}

test_pending_mr_skips_restart() {
  setup_case
  add_polecat delta session-dead
  printf 'PENDING_MR\n' > "$TEST_STATE/verdict/delta"
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "pending MR: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "pending MR: no restart mail"
  assert_file_contains "$TEST_STATE/output.log" "completed work" "pending MR: recognized as completed, not crashed"
  assert_file_contains "$TEST_STATE/output.log" "0 crashed, 0 stuck, 1 healthy" "pending MR: counted healthy"
}

test_safe_to_nuke_dead_agent_skips_restart() {
  setup_case
  add_polecat epsilon agent-dead
  printf 'SAFE_TO_NUKE\n' > "$TEST_STATE/verdict/epsilon"
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "safe-to-nuke: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "safe-to-nuke: no restart mail"
  assert_file_contains "$TEST_STATE/output.log" "completed work" "safe-to-nuke: recognized as completed"
}

test_needs_recovery_still_restarts() {
  setup_case
  add_polecat zeta session-dead
  printf 'NEEDS_RECOVERY\n' > "$TEST_STATE/verdict/zeta"
  run_script

  assert_line_count "$TEST_STATE/mail.log" 1 "needs-recovery: still restarts genuine crash"
  assert_file_contains "$TEST_STATE/mail.log" "RESTART_POLECAT: gastown/zeta" "needs-recovery: restart requested"
}

# The live storm shape (hq-2eqe): the polecat submitted an MR that is still open,
# but its work bead stays open so the recovery verdict reads NEEDS_RECOVERY
# (reconciled to stalled). The open active_mr must still be recognized as
# completed-and-awaiting-merge, NOT a crash.
test_open_active_mr_skips_restart() {
  setup_case
  add_polecat theta session-dead
  printf 'NEEDS_RECOVERY\n' > "$TEST_STATE/verdict/theta"
  printf 'mr-theta\n' > "$TEST_STATE/active_mr/theta"
  # mr-theta bead status defaults to "open" in the fake bd (pending merge).
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "open active_mr: no session kill"
  assert_file_empty "$TEST_STATE/mail.log" "open active_mr: no restart mail (submitted, awaiting merge)"
  assert_file_contains "$TEST_STATE/output.log" "completed work" "open active_mr: recognized as completed"
}

# A merged/closed active_mr is no longer pending, so it must NOT suppress a genuine
# restart — the open-MR skip is scoped to work still awaiting merge.
test_merged_active_mr_does_not_skip() {
  setup_case
  add_polecat kappa session-dead
  printf 'NEEDS_RECOVERY\n' > "$TEST_STATE/verdict/kappa"
  printf 'mr-kappa\n' > "$TEST_STATE/active_mr/kappa"
  printf 'closed\n' > "$TEST_STATE/status/mr-kappa"
  run_script

  assert_line_count "$TEST_STATE/mail.log" 1 "merged active_mr: still restarts (MR no longer pending)"
  assert_file_contains "$TEST_STATE/mail.log" "RESTART_POLECAT: gastown/kappa" "merged active_mr: restart requested"
}

test_mass_death_skips_actions() {
  setup_case
  add_polecat alpha agent-dead
  add_polecat beta agent-dead
  add_polecat gamma agent-dead
  run_script

  assert_file_empty "$TEST_STATE/kill.log" "mass death: no session kills"
  assert_file_empty "$TEST_STATE/mail.log" "mass death: no restart mail"
  assert_line_count "$TEST_STATE/escalate.log" 1 "mass death: one escalation"
  assert_file_contains "$TEST_STATE/output.log" "Skipping per-agent restart/kill actions" "mass death: action loops skipped"
}

test_mass_death_recheck_suppresses_recovered() {
  setup_case
  # Three polecats look dead during the enumeration scan...
  add_polecat alpha agent-dead
  add_polecat beta agent-dead
  add_polecat gamma agent-dead
  # ...but two recover (e.g. the witness restarted them) by the time the
  # pre-escalation live re-check runs.
  printf 'healthy\n' > "$TEST_STATE/health_after/gt-beta"
  printf 'healthy\n' > "$TEST_STATE/health_after/gt-gamma"
  run_script

  assert_file_empty "$TEST_STATE/escalate.log" "recheck: recovered agents drop below threshold, no mass-death escalation"
  assert_file_contains "$TEST_STATE/output.log" "dropped to 1 after live re-check" "recheck: logged the drop below threshold"
  assert_file_contains "$TEST_STATE/output.log" "recovered before mass-death escalation" "recheck: noted the recovered agents"
  # The one still-down agent is restarted normally instead of being suppressed.
  assert_file_contains "$TEST_STATE/mail.log" "RESTART_POLECAT: gastown/alpha" "recheck: still-down agent restarted normally"
}

test_mass_death_recheck_confirms_persistent() {
  setup_case
  # All three remain dead through the re-check (no health_after overrides).
  add_polecat alpha agent-dead
  add_polecat beta agent-dead
  add_polecat gamma agent-dead
  run_script

  assert_line_count "$TEST_STATE/escalate.log" 1 "persistent mass death: one escalation after re-check"
  assert_file_contains "$TEST_STATE/escalate.log" "Mass agent death: 3 agents down" "persistent mass death: confirmed count in title"
  assert_file_empty "$TEST_STATE/mail.log" "persistent mass death: restarts suppressed"
}

test_healthy_runtime opencode
test_healthy_runtime bun
test_healthy_runtime node
test_healthy_runtime claude
test_long_research_active_pane
test_dead_agent_restarts_one
test_dead_session_restarts_one
test_closed_hook_skips_restart
test_pending_mr_skips_restart
test_safe_to_nuke_dead_agent_skips_restart
test_needs_recovery_still_restarts
test_open_active_mr_skips_restart
test_merged_active_mr_does_not_skip
test_mass_death_skips_actions
test_mass_death_recheck_suppresses_recovered
test_mass_death_recheck_confirms_persistent

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
