#!/usr/bin/env bash
# scripts/upgrade-anvil.sh — upgrade anvil to latest upstream and replay patches
#
# Usage: make upgrade
#
# What it does:
#   1. Guards: clean tree, correct branch
#   2. Shows what's incoming from upstream
#   3. Resets anvil to upstream/main
#   4. Replays patches/*.patch via git am
#   5. On conflict: prints clear resume/abort instructions and exits 1
#   6. On success: offers to build and install

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

INCOMING=$(git log --oneline HEAD..upstream/main 2>/dev/null)
INCOMING_COUNT=$(echo "$INCOMING" | grep -c . || true)

if [ "$INCOMING_COUNT" -eq 0 ]; then
    ok "Already up to date with upstream/main"
    exit 0
fi

echo ""
echo -e "${BLU}Incoming from upstream ($INCOMING_COUNT commits):${RST}"
echo "$INCOMING" | while read -r line; do dim "  $line"; done
echo ""

# ── Reset and replay ─────────────────────────────────────────────────────────

PATCH_TMPDIR=$(mktemp -d)
trap 'rm -rf "$PATCH_TMPDIR"' EXIT
cp -f patches/*.patch "$PATCH_TMPDIR/"

info "Resetting anvil to upstream/main..."
git reset --hard upstream/main --quiet
ok "Reset to $(git rev-parse --short HEAD) ($(git log -1 --format='%s'))"

echo ""
info "Replaying $PATCH_COUNT local patches..."
echo ""

PATCH_NUM=0
for patch in "$PATCH_TMPDIR"/*.patch; do
    PATCH_NUM=$((PATCH_NUM + 1))
    SUBJECT=$(grep '^Subject:' "$patch" | sed 's/Subject: \[PATCH[^]]*\] //')
    printf "  ${DIM}Applying %d/%d: %s${RST}\n" "$PATCH_NUM" "$PATCH_COUNT" "$SUBJECT"
done
echo ""

if ! git am --3way "$PATCH_TMPDIR"/*.patch; then
    echo ""
    die "$(cat <<'EOF'
Patch conflict — resolve then continue:

  1. Edit the conflicting file(s)
  2. git add <file>
  3. git am --continue

To bail out entirely:
  git am --abort
  git reset --hard upstream/main
EOF
)"
fi

echo ""
ok "All $PATCH_COUNT patches applied"
ok "anvil is now at $(git rev-parse --short HEAD)"
echo ""

# ── Optional build ───────────────────────────────────────────────────────────

if [ -t 0 ]; then
    read -rp "$(echo -e "${BLU}▸${RST} Build and install now? [Y/n] ")" REPLY
    REPLY=${REPLY:-Y}
    if [[ "$REPLY" =~ ^[Yy]$ ]]; then
        echo ""
        make install
    fi
else
    dim "  (non-interactive — skipping build prompt)"
    dim "  Run 'make install' when ready"
fi

echo ""
ok "Upgrade complete."
