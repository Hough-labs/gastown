package cmd

import (
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

func mrIssue(id, description string) *beads.Issue {
	return &beads.Issue{
		ID:          id,
		Title:       "Merge " + id,
		Status:      "open",
		Description: description,
	}
}

func TestBlockingOpenMR_NoMRs(t *testing.T) {
	if id, blocked := blockingOpenMR(nil); blocked {
		t.Fatalf("nil MR list should not block, got %q", id)
	}
	if id, blocked := blockingOpenMR([]*beads.Issue{}); blocked {
		t.Fatalf("empty MR list should not block, got %q", id)
	}
	if id, blocked := blockingOpenMR([]*beads.Issue{nil}); blocked {
		t.Fatalf("nil MR entry should not block, got %q", id)
	}
}

func TestBlockingOpenMR_CleanMRBlocks(t *testing.T) {
	// The an-abn shape (gfork-649): gt done created a clean MR, the PR is
	// open awaiting review — re-dispatch must be blocked.
	mr := mrIssue("an-wisp-nglv", "branch: polecat/nux/an-abn\nsource_issue: an-abn\nworker: polecats/nux\nrig: android\ncommit_sha: abc123\n")
	id, blocked := blockingOpenMR([]*beads.Issue{mr})
	if !blocked {
		t.Fatal("clean open MR must block dispatch")
	}
	if id != "an-wisp-nglv" {
		t.Fatalf("expected blocking MR an-wisp-nglv, got %q", id)
	}
}

func TestBlockingOpenMR_DistressedMRsAllowTakeover(t *testing.T) {
	// Prior-attempt takeover (GH#gt-zqvj) stays possible: MRs with
	// conflict/retry/close evidence do not block re-dispatch.
	cases := []struct {
		name string
		desc string
	}{
		{"retry_count", "branch: b\nsource_issue: gt-1\nretry_count: 2\n"},
		{"conflict_task", "branch: b\nsource_issue: gt-1\nconflict_task_id: gt-cfx\n"},
		{"close_reason", "branch: b\nsource_issue: gt-1\nclose_reason: rejected\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if id, blocked := blockingOpenMR([]*beads.Issue{mrIssue("gt-wisp-1", tc.desc)}); blocked {
				t.Fatalf("distressed MR (%s) should not block, got %q", tc.name, id)
			}
		})
	}
}

func TestBlockingOpenMR_MixedDistressedAndClean(t *testing.T) {
	distressed := mrIssue("gt-wisp-old", "branch: b1\nsource_issue: gt-1\nretry_count: 1\n")
	clean := mrIssue("gt-wisp-new", "branch: b2\nsource_issue: gt-1\ncommit_sha: def456\n")
	id, blocked := blockingOpenMR([]*beads.Issue{distressed, clean})
	if !blocked {
		t.Fatal("a clean MR alongside distressed ones must still block")
	}
	if id != "gt-wisp-new" {
		t.Fatalf("expected clean MR gt-wisp-new to block, got %q", id)
	}
}

func TestBlockingOpenMR_UnparseableFieldsBlocksConservatively(t *testing.T) {
	// An open gt:merge-request bead without parseable key: value fields still
	// represents a submission — fail closed on the dispatch side.
	mr := mrIssue("gt-wisp-junk", "free-form text with no fields")
	if _, blocked := blockingOpenMR([]*beads.Issue{mr}); !blocked {
		t.Fatal("unparseable MR fields should block conservatively")
	}
}

func closedMR(id, closeReason string, closedAt time.Time) *beads.Issue {
	iss := &beads.Issue{
		ID:          id,
		Title:       "Merge " + id,
		Status:      "closed",
		Description: "branch: b\nsource_issue: gt-1\nclose_reason: " + closeReason + "\n",
	}
	if !closedAt.IsZero() {
		iss.ClosedAt = closedAt.Format(time.RFC3339)
	}
	return iss
}

func TestRecentlyResubmittingMR_BlocksWithinGraceWindow(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for _, reason := range []string{"rejected", "superseded"} {
		t.Run(reason, func(t *testing.T) {
			mr := closedMR("gt-wisp-rej", reason, now.Add(-2*time.Minute))
			id, blocked := recentlyResubmittingMR([]*beads.Issue{mr}, now)
			if !blocked {
				t.Fatalf("recent %s MR must block during resubmit window", reason)
			}
			if id != "gt-wisp-rej" {
				t.Fatalf("expected gt-wisp-rej, got %q", id)
			}
		})
	}
}

func TestRecentlyResubmittingMR_StaleRejectAllowsTakeover(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	mr := closedMR("gt-wisp-old", "rejected", now.Add(-mrResubmitGraceWindow-time.Minute))
	if id, blocked := recentlyResubmittingMR([]*beads.Issue{mr}, now); blocked {
		t.Fatalf("a reject older than the grace window must not block, got %q", id)
	}
}

func TestRecentlyResubmittingMR_NonResubmitReasonsDoNotBlock(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for _, reason := range []string{"merged", "conflict", ""} {
		t.Run("reason="+reason, func(t *testing.T) {
			mr := closedMR("gt-wisp-x", reason, now.Add(-time.Minute))
			if id, blocked := recentlyResubmittingMR([]*beads.Issue{mr}, now); blocked {
				t.Fatalf("close_reason %q should not block dispatch, got %q", reason, id)
			}
		})
	}
}

func TestRecentlyResubmittingMR_UnparseableCloseTimeFailsOpen(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	mr := &beads.Issue{
		ID:          "gt-wisp-notime",
		Status:      "closed",
		Description: "branch: b\nsource_issue: gt-1\nclose_reason: rejected\n",
		ClosedAt:    "not-a-timestamp",
		UpdatedAt:   "also-bad",
	}
	if id, blocked := recentlyResubmittingMR([]*beads.Issue{mr}, now); blocked {
		t.Fatalf("unparseable close time should fail open, got %q", id)
	}
}

func TestRecentlyResubmittingMR_FallsBackToUpdatedAt(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	mr := &beads.Issue{
		ID:          "gt-wisp-upd",
		Status:      "closed",
		Description: "branch: b\nsource_issue: gt-1\nclose_reason: rejected\n",
		UpdatedAt:   now.Add(-time.Minute).Format(time.RFC3339),
	}
	if _, blocked := recentlyResubmittingMR([]*beads.Issue{mr}, now); !blocked {
		t.Fatal("a recent reject with only updated_at set must still block")
	}
}

func TestOpenMRDispatchBlock_UnroutablePrefixFailsOpen(t *testing.T) {
	// No routes.jsonl in an empty town root → prefix unresolvable → no block
	// (fail open; gt done's close guard is the false-close backstop).
	townRoot := t.TempDir()
	if id, blocked := openMRDispatchBlock(townRoot, "zz-123"); blocked {
		t.Fatalf("unroutable prefix should fail open, got %q", id)
	}
	if id, blocked := openMRDispatchBlock(townRoot, "noprefix"); blocked {
		t.Fatalf("prefixless ID should fail open, got %q", id)
	}
}
