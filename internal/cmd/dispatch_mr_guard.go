package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// mrResubmitGraceWindow is how long after an MR is rejected/superseded the
// dispatch guard keeps blocking re-dispatch of the source issue (gfork-dk6).
// Reject CLOSES the MR bead, so the source issue momentarily has no OPEN MR and
// the open-MR guard fails open — the deacon then dispatches a duplicate polecat
// before the owner (the nudged worker, or the refinery) resubmits a fresh MR.
// Once the window elapses with no resubmit, the work is treated as abandoned and
// takeover proceeds as before.
const mrResubmitGraceWindow = 10 * time.Minute

// blockingOpenMR classifies open merge-request beads for dispatch gating
// (gfork-649). A clean open MR — no conflict/retry/close evidence — means the
// source issue's work is already submitted and awaiting merge/review, so
// re-dispatching the issue would spawn a duplicate polecat against work the
// refinery owns. Distressed MRs (conflict task attached, retries recorded, or
// a close_reason set) are legitimate prior-attempt takeover targets
// (GH#gt-zqvj) and do not block.
func blockingOpenMR(mrs []*beads.Issue) (string, bool) {
	for _, mr := range mrs {
		if mr == nil {
			continue
		}
		f := beads.ParseMRFields(mr)
		if f == nil {
			// An open gt:merge-request bead without parseable fields still
			// represents a submission — block conservatively.
			return mr.ID, true
		}
		if f.RetryCount > 0 || f.ConflictTaskID != "" || f.CloseReason != "" {
			continue
		}
		return mr.ID, true
	}
	return "", false
}

// openMRDispatchBlock reports whether issueID has a clean open merge request
// that should block re-dispatch, resolving the issue's rig beads DB from its
// prefix (MR beads live in the rig DB, not the town DB). Lookup failures fail
// OPEN (no block): dispatch guards must degrade to current behavior when Dolt
// is unavailable — gt done's open-MR close guard is the backstop against
// false-closes.
func openMRDispatchBlock(townRoot, issueID string) (string, bool) {
	prefix := beads.ExtractPrefix(issueID)
	if prefix == "" {
		return "", false
	}
	rig := beads.GetRigNameForPrefix(townRoot, prefix)
	if rig == "" {
		return "", false // town-level bead — not part of the rig MR flow
	}
	bd := beads.New(filepath.Join(townRoot, rig))
	mrs, err := bd.FindOpenMRsForIssue(issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: open-MR dispatch check for %s failed (proceeding without guard): %v\n", issueID, err)
		return "", false
	}
	if mrID, blocked := blockingOpenMR(mrs); blocked {
		return mrID, true
	}

	// gfork-dk6: a rejected/superseded MR is CLOSED, so it never appears in the
	// open-MR list above — the source issue looks ready and the deacon spawns a
	// duplicate polecat before the owner resubmits. Hold dispatch for a grace
	// window after a recent reject so the resubmit can land first.
	closed, err := bd.FindClosedMRsForIssue(issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: closed-MR dispatch check for %s failed (proceeding without guard): %v\n", issueID, err)
		return "", false
	}
	return recentlyResubmittingMR(closed, time.Now())
}

// recentlyResubmittingMR reports whether issueID has a merge request that was
// rejected or superseded within mrResubmitGraceWindow of now, i.e. an owner is
// expected to resubmit imminently and a fresh dispatch would duplicate it
// (gfork-dk6). Merged/conflict closures and stale (past-window) rejects do not
// block. An unparseable close timestamp fails OPEN, consistent with the rest of
// this guard — gt done's open-MR close guard is the backstop.
func recentlyResubmittingMR(mrs []*beads.Issue, now time.Time) (string, bool) {
	for _, mr := range mrs {
		if mr == nil {
			continue
		}
		f := beads.ParseMRFields(mr)
		if f == nil {
			continue
		}
		if f.CloseReason != "rejected" && f.CloseReason != "superseded" {
			continue
		}
		closedAt := mrCloseTime(mr)
		if closedAt.IsZero() {
			continue
		}
		if now.Sub(closedAt) <= mrResubmitGraceWindow {
			return mr.ID, true
		}
	}
	return "", false
}

// mrCloseTime resolves when an MR bead reached its terminal state, preferring
// the explicit closed_at and falling back to updated_at (the reject updates the
// bead). Returns the zero time if neither is a parseable RFC3339 timestamp.
func mrCloseTime(mr *beads.Issue) time.Time {
	for _, ts := range []string{mr.ClosedAt, mr.UpdatedAt} {
		if ts == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}
	return time.Time{}
}
