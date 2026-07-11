package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/beads"
)

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
	return blockingOpenMR(mrs)
}
