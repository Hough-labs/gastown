package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/deacon"
	"github.com/steveyegge/gastown/internal/wisp"
)

func TestOutputRoleDirectives(t *testing.T) {
	t.Parallel()

	t.Run("no directives emits nothing visible", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()
		ctx := RoleContext{
			Role:     RolePolecat,
			TownRoot: townRoot,
			Rig:      "myrig",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, false)
		out := buf.String()

		if strings.Contains(out, "Directives") {
			t.Errorf("expected no header when no directives, got: %s", out)
		}
	})

	t.Run("town-level directive emits town header", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()
		dir := filepath.Join(townRoot, "directives")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "polecat.md"), []byte("Always be polite."), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx := RoleContext{
			Role:     RolePolecat,
			TownRoot: townRoot,
			Rig:      "myrig",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, false)
		out := buf.String()

		if !strings.Contains(out, "## Town Directives") {
			t.Errorf("expected Town Directives header, got: %s", out)
		}
		if !strings.Contains(out, "Always be polite.") {
			t.Errorf("expected directive content, got: %s", out)
		}
	})

	t.Run("rig-level directive emits rig header", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()
		dir := filepath.Join(townRoot, "myrig", "directives")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "witness.md"), []byte("Watch closely."), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx := RoleContext{
			Role:     RoleWitness,
			TownRoot: townRoot,
			Rig:      "myrig",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, false)
		out := buf.String()

		if !strings.Contains(out, "## Rig Directives") {
			t.Errorf("expected Rig Directives header, got: %s", out)
		}
		if !strings.Contains(out, "Watch closely.") {
			t.Errorf("expected directive content, got: %s", out)
		}
	})

	t.Run("both levels emits combined header", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()

		townDir := filepath.Join(townRoot, "directives")
		if err := os.MkdirAll(townDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(townDir, "polecat.md"), []byte("Town rule."), 0o644); err != nil {
			t.Fatal(err)
		}

		rigDir := filepath.Join(townRoot, "myrig", "directives")
		if err := os.MkdirAll(rigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rigDir, "polecat.md"), []byte("Rig rule."), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx := RoleContext{
			Role:     RolePolecat,
			TownRoot: townRoot,
			Rig:      "myrig",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, false)
		out := buf.String()

		if !strings.Contains(out, "## Town & Rig Directives") {
			t.Errorf("expected combined header, got: %s", out)
		}
		if !strings.Contains(out, "Town rule.") {
			t.Errorf("expected town content, got: %s", out)
		}
		if !strings.Contains(out, "Rig rule.") {
			t.Errorf("expected rig content, got: %s", out)
		}
	})

	t.Run("explain mode shows file paths", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()

		ctx := RoleContext{
			Role:     RolePolecat,
			TownRoot: townRoot,
			Rig:      "myrig",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, true)
		out := buf.String()

		if !strings.Contains(out, "[EXPLAIN]") {
			t.Errorf("expected EXPLAIN output, got: %s", out)
		}
		if !strings.Contains(out, filepath.Join("directives", "polecat.md")) {
			t.Errorf("expected file path in explain output, got: %s", out)
		}
	})

	t.Run("empty rig name skips rig path", func(t *testing.T) {
		t.Parallel()
		townRoot := t.TempDir()

		townDir := filepath.Join(townRoot, "directives")
		if err := os.MkdirAll(townDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(townDir, "mayor.md"), []byte("Mayor directive."), 0o644); err != nil {
			t.Fatal(err)
		}

		ctx := RoleContext{
			Role:     RoleMayor,
			TownRoot: townRoot,
			Rig:      "",
		}

		var buf bytes.Buffer
		outputRoleDirectives(ctx, &buf, false)
		out := buf.String()

		if !strings.Contains(out, "## Town Directives") {
			t.Errorf("expected Town Directives header, got: %s", out)
		}
		if !strings.Contains(out, "Mayor directive.") {
			t.Errorf("expected directive content, got: %s", out)
		}
	})
}

func TestOutputCommandQuickReferenceBootBlocksRawTmux(t *testing.T) {
	output := captureStdout(t, func() {
		outputCommandQuickReference(RoleContext{Role: RoleBoot})
	})

	for _, want := range []string{
		"gt nudge deacon",
		"blocked; can stage unsubmitted input",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("Boot quick reference missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "tmux send-keys~~ (unreliable)") {
		t.Fatalf("Boot quick reference still calls raw tmux merely unreliable:\n%s", output)
	}
}

// TestOutputPrimeContextLite covers the Lever A steady-state patrol prime
// (gfork-47p.4): under primeLiteMode, deacon/witness get a ~5-line identity
// block instead of the full role template.
func TestOutputPrimeContextLite(t *testing.T) {
	origLite := primeLiteMode
	defer func() { primeLiteMode = origLite }()

	t.Run("deacon gets lite block with formula pointer", func(t *testing.T) {
		primeLiteMode = true
		ctx := RoleContext{Role: RoleDeacon, Rig: ""}

		output := captureStdout(t, func() {
			formula, err := outputPrimeContext(ctx)
			if err != nil {
				t.Fatalf("outputPrimeContext: %v", err)
			}
			if formula != "" {
				t.Errorf("expected empty formula return for lite path, got %q", formula)
			}
		})

		if !strings.Contains(output, "Deacon") {
			t.Errorf("expected identity to mention Deacon, got:\n%s", output)
		}
		if !strings.Contains(output, "gt prime") {
			t.Errorf("expected pointer to full 'gt prime', got:\n%s", output)
		}
		if !strings.Contains(output, "gt formula show "+constants.MolDeaconPatrol) {
			t.Errorf("expected pointer to full step text via formula show, got:\n%s", output)
		}
		if len(output) > 500 {
			t.Errorf("lite prime context should be small (~5 lines), got %d bytes:\n%s", len(output), output)
		}
	})

	t.Run("witness gets lite block with formula pointer and rig name", func(t *testing.T) {
		primeLiteMode = true
		ctx := RoleContext{Role: RoleWitness, Rig: "gastown"}

		output := captureStdout(t, func() {
			outputPrimeContext(ctx)
		})

		if !strings.Contains(output, "Witness") {
			t.Errorf("expected identity to mention Witness, got:\n%s", output)
		}
		if !strings.Contains(output, "gastown") {
			t.Errorf("expected rig name in identity, got:\n%s", output)
		}
		if !strings.Contains(output, "gt formula show "+constants.MolWitnessPatrol) {
			t.Errorf("expected pointer to full step text via formula show, got:\n%s", output)
		}
	})

	t.Run("refinery is carved out — full template even under primeLiteMode", func(t *testing.T) {
		primeLiteMode = true
		ctx := RoleContext{Role: RoleRefinery, Rig: "gastown"}

		output := captureStdout(t, func() {
			outputPrimeContext(ctx)
		})

		if strings.Contains(output, "steady-state patrol cycle") {
			t.Errorf("refinery must NOT get the lite prime context under primeLiteMode, got:\n%s", output)
		}
	})

	t.Run("no primeLiteMode — deacon gets full template, not the lite block", func(t *testing.T) {
		primeLiteMode = false
		ctx := RoleContext{Role: RoleDeacon, Rig: ""}

		output := captureStdout(t, func() {
			outputPrimeContext(ctx)
		})

		if strings.Contains(output, "steady-state patrol cycle") {
			t.Errorf("deacon should get the full template when primeLiteMode is false, got:\n%s", output)
		}
	})
}

// TestOutputStartupDirectiveLite covers the Lever A startup-directive tier
// under primeLiteMode: deacon/witness collapse to "Begin patrol at step 1.",
// refinery keeps its full startup protocol (carve-out), and the parked/
// paused correctness gates are preserved even in the lite tier.
func TestOutputStartupDirectiveLite(t *testing.T) {
	origLite := primeLiteMode
	defer func() { primeLiteMode = origLite }()

	t.Run("witness not parked: collapses to Begin patrol", func(t *testing.T) {
		primeLiteMode = true
		townRoot := t.TempDir()
		ctx := RoleContext{Role: RoleWitness, TownRoot: townRoot, Rig: "gastown"}

		output := captureStdout(t, func() {
			outputStartupDirective(ctx)
		})

		if !strings.Contains(output, "Begin patrol at step 1.") {
			t.Errorf("expected lite startup directive, got:\n%s", output)
		}
		if strings.Contains(output, "STARTUP PROTOCOL") {
			t.Errorf("expected the full STARTUP PROTOCOL block to be suppressed, got:\n%s", output)
		}
	})

	t.Run("witness parked: parked message preserved even in lite tier", func(t *testing.T) {
		primeLiteMode = true
		townRoot := t.TempDir()
		rigName := "testrig"

		configDir := filepath.Join(townRoot, wisp.WispConfigDir, wisp.ConfigSubdir)
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatalf("create wisp config dir: %v", err)
		}
		configFile := filepath.Join(configDir, rigName+".json")
		data, _ := json.Marshal(wisp.ConfigFile{
			Rig:    rigName,
			Values: map[string]interface{}{"status": "parked"},
		})
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatalf("write wisp config: %v", err)
		}

		ctx := RoleContext{Role: RoleWitness, TownRoot: townRoot, Rig: rigName}
		output := captureStdout(t, func() {
			outputStartupDirective(ctx)
		})

		if !strings.Contains(output, "No patrol needed. Exit cleanly.") {
			t.Errorf("parked-rig gate must be preserved under primeLiteMode, got:\n%s", output)
		}
		if strings.Contains(output, "Begin patrol at step 1.") {
			t.Errorf("parked rig should not also get the patrol-start line, got:\n%s", output)
		}
	})

	t.Run("deacon not paused: collapses to Begin patrol", func(t *testing.T) {
		primeLiteMode = true
		townRoot := t.TempDir()
		ctx := RoleContext{Role: RoleDeacon, TownRoot: townRoot}

		output := captureStdout(t, func() {
			outputStartupDirective(ctx)
		})

		if !strings.Contains(output, "Begin patrol at step 1.") {
			t.Errorf("expected lite startup directive, got:\n%s", output)
		}
	})

	t.Run("deacon paused: pause gate preserved, no output", func(t *testing.T) {
		primeLiteMode = true
		townRoot := t.TempDir()
		if err := deacon.Pause(townRoot, "test", "human"); err != nil {
			t.Fatalf("deacon.Pause: %v", err)
		}
		ctx := RoleContext{Role: RoleDeacon, TownRoot: townRoot}

		output := captureStdout(t, func() {
			outputStartupDirective(ctx)
		})

		if output != "" {
			t.Errorf("paused deacon should print nothing under primeLiteMode, got:\n%s", output)
		}
	})

	t.Run("refinery carve-out: full startup protocol even under primeLiteMode", func(t *testing.T) {
		primeLiteMode = true
		townRoot := t.TempDir()
		ctx := RoleContext{Role: RoleRefinery, TownRoot: townRoot, Rig: "gastown"}

		if handled := outputStartupDirectiveLite(ctx); handled {
			t.Fatalf("outputStartupDirectiveLite should not handle refinery (carve-out)")
		}

		output := captureStdout(t, func() {
			outputStartupDirective(ctx)
		})

		if !strings.Contains(output, "STARTUP PROTOCOL") || !strings.Contains(output, "Refinery") {
			t.Errorf("refinery must keep its full startup protocol under primeLiteMode, got:\n%s", output)
		}
		if strings.Contains(output, "Begin patrol at step 1.") {
			t.Errorf("refinery should not get the lite patrol-start line, got:\n%s", output)
		}
	})
}
