package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

// TestLoadRigCommandVars_RequireReviewBothValues guards gfork-a84: the sling
// path (loadRigCommandVars) must emit require_review for BOTH true and false,
// matching the patrol path (buildRefineryPatrolVars). Previously it emitted the
// var only when enabled, leaving the formula default as the single source of
// truth for the disabled case — a silent over-require risk if that default ever
// flips to true.
func TestLoadRigCommandVars_RequireReviewBothValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{"enabled emits true", true, "true"},
		{"disabled emits false", false, "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			townRoot := t.TempDir()
			rig := "testrig"
			settingsDir := filepath.Join(townRoot, rig, "settings")
			if err := os.MkdirAll(settingsDir, 0o755); err != nil {
				t.Fatal(err)
			}

			enabled := tc.enabled
			settings := config.RigSettings{
				Type:    "rig-settings",
				Version: 1,
				MergeQueue: &config.MergeQueueConfig{
					Enabled:       true,
					RequireReview: &enabled,
				},
			}
			data, _ := json.Marshal(settings)
			if err := os.WriteFile(filepath.Join(settingsDir, "config.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}

			vars := loadRigCommandVars(townRoot, rig)

			varMap := make(map[string]string)
			for _, v := range vars {
				parts := splitFirstEquals(v)
				if len(parts) == 2 {
					varMap[parts[0]] = parts[1]
				}
			}

			got, ok := varMap["require_review"]
			if !ok {
				t.Fatalf("require_review var not emitted (disabled case regressed to omit); vars=%v", vars)
			}
			if got != tc.want {
				t.Errorf("require_review = %q, want %q", got, tc.want)
			}
		})
	}
}
