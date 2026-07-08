package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/formula"
)

// TestReviewPRFormula_DeclaresReviewOnly guards that the PR-review formula
// declares review_only=true, matching its sibling review formulas
// (mol-plan-review, mol-prd-review, mol-polecat-code-review). Without this
// marker, a dispatched reviewer's bead never gets review_only, so gt done's
// verified_push gate wedges the reviewer. See gfork-06a / an-1af.
func TestReviewPRFormula_DeclaresReviewOnly(t *testing.T) {
	content, err := formula.GetEmbeddedFormulaContent("mol-polecat-review-pr")
	if err != nil {
		t.Fatalf("GetEmbeddedFormulaContent: %v", err)
	}
	f, err := formula.Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.ReviewOnly {
		t.Error("mol-polecat-review-pr must declare review_only=true — reviewers make no code commits (gfork-06a)")
	}
}

// TestFormulaDeclaresReviewOnly covers the helper that lets any dispatch path
// derive review_only from the attached formula (not just convoy legs).
func TestFormulaDeclaresReviewOnly(t *testing.T) {
	tests := []struct {
		name    string
		formula string
		want    bool
	}{
		{"review-pr is review-only", "mol-polecat-review-pr", true},
		{"code-review is review-only", "mol-polecat-code-review", true},
		{"polecat-work is not review-only", "mol-polecat-work", false},
		{"unknown formula falls back to false", "does-not-exist", false},
		{"empty name falls back to false", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formulaDeclaresReviewOnly(tt.formula); got != tt.want {
				t.Errorf("formulaDeclaresReviewOnly(%q) = %v, want %v", tt.formula, got, tt.want)
			}
		})
	}
}

// TestStoreFieldsInBead_DerivesReviewOnlyFromFormula proves that dispatching a
// review-only formula stamps review_only on the bead even when the caller does
// not pass the explicit --review-only flag (the iris single-reviewer path).
// The GT_TEST_ATTACHED_MOLECULE_LOG seam renders the attachment fields to a
// file instead of hitting bd, so this stays hermetic.
func TestStoreFieldsInBead_DerivesReviewOnlyFromFormula(t *testing.T) {
	tests := []struct {
		name            string
		attachedFormula string
		explicitReview  bool
		wantReviewOnly  bool
	}{
		{"review-pr formula derives review_only without flag", "mol-polecat-review-pr", false, true},
		{"explicit flag still wins for non-review formula", "mol-polecat-work", true, true},
		{"non-review formula without flag stays off", "mol-polecat-work", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "attach.log")
			t.Setenv("GT_TEST_ATTACHED_MOLECULE_LOG", logPath)

			if err := storeFieldsInBead("an-test", beadFieldUpdates{
				AttachedFormula: tt.attachedFormula,
				ReviewOnly:      tt.explicitReview,
			}); err != nil {
				t.Fatalf("storeFieldsInBead: %v", err)
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read rendered attachment log: %v", err)
			}
			got := strings.Contains(string(data), "review_only: true")
			if got != tt.wantReviewOnly {
				t.Errorf("review_only present = %v, want %v\nrendered:\n%s", got, tt.wantReviewOnly, data)
			}
		})
	}
}
