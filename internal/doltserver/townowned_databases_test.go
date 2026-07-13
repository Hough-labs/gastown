package doltserver

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTownOwnedDatabases verifies that townOwnedDatabases scopes to the town's
// own databases (hq + registered rigs + routes) and does NOT pick up unrelated
// project checkouts that happen to live under the town root — the distinction
// that keeps ListDatabases town-scoped when connected to a shared external
// Dolt server (hq-lggo).
func TestTownOwnedDatabases(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meta := func(db string) string { return `{"dolt_database":"` + db + `"}` }

	// hq (town-level beads).
	write(".beads/metadata.json", meta("hq"))
	// Registered rigs in rigs.json: android (rig-root .beads) + feryn (mayor/rig).
	write("mayor/rigs.json", `{"rigs":{"android":{},"feryn":{}}}`)
	write("android/.beads/metadata.json", meta("android"))
	write("feryn/mayor/rig/.beads/metadata.json", meta("feryn"))
	// gauntlet is reachable only via a route (not in rigs.json) — must still be found.
	write(".beads/routes.jsonl", `{"prefix":"gaunt-","path":"gauntlet/mayor/rig"}`+"\n")
	write("gauntlet/mayor/rig/.beads/metadata.json", meta("gauntlet"))
	// An unrelated project checkout under the town root — must NOT be treated as
	// a town database even though it has its own .beads/metadata.json.
	write("mill/.beads/metadata.json", meta("mill"))

	got := townOwnedDatabases(root)

	for _, want := range []string{"hq", "android", "feryn", "gauntlet"} {
		if !got[want] {
			t.Errorf("expected town-owned database %q, missing from %v", want, got)
		}
	}
	if got["mill"] {
		t.Errorf("unrelated project database %q leaked into town-owned set %v", "mill", got)
	}
	if len(got) != 4 {
		t.Errorf("got %d town-owned databases %v, want exactly 4 (hq, android, feryn, gauntlet)", len(got), got)
	}
}
