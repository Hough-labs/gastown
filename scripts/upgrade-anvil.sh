#!/usr/bin/env bash
# scripts/upgrade-anvil.sh — rebase local patches onto latest upstream.
#
# Usage: make upgrade
#
# Rebase-based (no local reset --hard). Remote anvil still gets reset --hard to
# origin/anvil via the pre-push hook — that's required to keep the remote
# checkout in sync with origin, and happens over ssh, not locally.
#
# Flow:
#   1. Guards: clean tree, correct branch, upstream reachable
#   2. Fetch upstream, show incoming commits
#   3. git rebase upstream/main (preserves local patch commits; conflicts pause)
#   4. make patches (regen from new SHAs) + amend last commit if diff
#   5. Optional interactive push (force-with-lease) to trigger deploy

set -euo pipefail

RED='\033[0;31m'
GRN='\033[0;32m'
YLW='\033[0;33m'
BLU='\033[0;34m'
DIM='\033[2m'
RST='\033[0m'

info()  { echo -e "${BLU}▸${RST} $*"; }
ok()    { echo -e "${GRN}✓${RST} $*"; }
warn()  { echo -e "${YLW}⚠${RST} $*"; }
die()   { echo -e "${RED}✗${RST} $*" >&2; exit 1; }
dim()   { echo -e "${DIM}$*${RST}"; }

# ── Guards ───────────────────────────────────────────────────────────────────

BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null) || die "Not on a git branch"
[ "$BRANCH" = "anvil" ] || die "Must be on the anvil branch (currently on: $BRANCH)"

if ! git diff --quiet || ! git diff --cached --quiet; then
    die "Working tree is dirty. Commit or stash changes before upgrading."
fi

PATCH_COUNT=$(find patches/ -maxdepth 1 -name '*.patch' 2>/dev/null | wc -l | tr -d ' ')
[ "$PATCH_COUNT" -gt 0 ] || die "No patches found in patches/ — run 'make patches' first"

# ── Fetch and preview ────────────────────────────────────────────────────────

info "Fetching upstream..."
git fetch upstream --quiet

INCOMING=$(git log --oneline HEAD..upstream/main 2>/dev/null || true)
INCOMING_COUNT=$(echo -n "$INCOMING" | grep -c '^' || true)

if [ "$INCOMING_COUNT" -eq 0 ]; then
    ok "Already up to date with upstream/main"
    exit 0
fi

echo ""
echo -e "${BLU}Incoming from upstream ($INCOMING_COUNT commits):${RST}"
echo "$INCOMING" | head -20 | while read -r line; do dim "  $line"; done
[ "$INCOMING_COUNT" -gt 20 ] && dim "  ... and $((INCOMING_COUNT - 20)) more"
echo ""

# ── Rebase local patches onto upstream/main ──────────────────────────────────

LOCAL_COMMITS=$(git rev-list --count upstream/main..HEAD)
info "Rebasing $LOCAL_COMMITS local patch commit(s) onto upstream/main..."
echo ""

if ! git rebase upstream/main; then
    echo ""
    die "$(cat <<'EOF'
Rebase conflict — resolve then continue:

  1. Edit the conflicting file(s)
  2. git add <file>
  3. git rebase --continue

To bail out (safe — no reset --hard has occurred):
  git rebase --abort
EOF
)"
fi

ok "All $LOCAL_COMMITS patches preserved — anvil now at $(git describe --tags HEAD 2>/dev/null || git rev-parse --short HEAD)"
echo ""

# ── Regenerate patches/ from new SHAs ────────────────────────────────────────

info "Regenerating patches/ from new SHAs..."
make patches >/dev/null

if ! git diff --quiet patches/; then
    info "Amending tip commit with refreshed patches/..."
    git add patches/
    git commit --amend --no-edit --quiet
    ok "Amended — tree clean"
else
    dim "  patches/ already current (no amend needed)"
fi
echo ""

# ── Optional push ────────────────────────────────────────────────────────────

if [ -t 0 ]; then
    read -rp "$(echo -e "${BLU}▸${RST} Push to origin and deploy (force-with-lease)? [Y/n] ")" REPLY
    REPLY=${REPLY:-Y}
    if [[ "$REPLY" =~ ^[Yy]$ ]]; then
        echo ""
        git push --force-with-lease origin anvil
        echo ""
        info "Anvil sync backgrounded — tail: ~/.cache/sync-anvil.log"
    else
        dim "  Skipped. Run: git push --force-with-lease origin anvil"
    fi
else
    dim "  (non-interactive — skipping push prompt)"
    dim "  Run: git push --force-with-lease origin anvil"
fi

echo ""
ok "Upgrade complete."
