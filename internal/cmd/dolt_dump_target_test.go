package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/doltserver"
)

// TestDoltDumpTargetLinesGTManaged verifies the gt-managed presentation is
// unchanged: it reports the gt-tracked PID and the config data-dir/log-file
// exactly as before the external-server fix.
func TestDoltDumpTargetLinesGTManaged(t *testing.T) {
	config := &doltserver.Config{
		Port:    3307,
		DataDir: "/Users/hunter/gt/.dolt-data",
		LogFile: "/Users/hunter/gt/daemon/dolt.log",
	}

	lines := doltDumpTargetLines(4242, config, doltserver.ExternalServerDiag{}, false)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "Live PID:   4242") {
		t.Errorf("gt-managed dump should report the gt PID 4242, got:\n%s", joined)
	}
	if !strings.Contains(joined, "/Users/hunter/gt/.dolt-data") {
		t.Errorf("gt-managed dump should show the config data dir, got:\n%s", joined)
	}
	if !strings.Contains(joined, "/Users/hunter/gt/daemon/dolt.log") {
		t.Errorf("gt-managed dump should show the config log file, got:\n%s", joined)
	}
	if strings.Contains(joined, "EXTERNAL") {
		t.Errorf("gt-managed dump must not claim the server is external, got:\n%s", joined)
	}
}

// TestDoltDumpTargetLinesExternal verifies the external (launchd/systemd)
// presentation: it reports the REAL listening PID and data dir (never PID 0 or
// the empty <townRoot>/.dolt-data shim), and never presents the frozen
// gt-managed log path as the live server's — the exact stale signals that
// triggered false "Dolt unreachable" escalations (hq-80nx/hq-89vh/hq-ysvn).
func TestDoltDumpTargetLinesExternal(t *testing.T) {
	config := &doltserver.Config{
		Port:    3307,
		DataDir: "/Users/hunter/gt/.dolt-data",           // empty post-migration shim
		LogFile: "/Users/hunter/gt/daemon/dolt.log",      // frozen dead-daemon log
	}
	ext := doltserver.ExternalServerDiag{
		PID:        85515,
		DataDir:    "/Users/hunter/.local/share/bastion/dolt/databases",
		ConfigPath: "/Users/hunter/.local/share/bastion/dolt/config.yaml",
	}

	// IsRunning reports PID 0 for an unmanaged server; the caller passes that 0.
	lines := doltDumpTargetLines(0, config, ext, true)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "85515") {
		t.Errorf("external dump should report the real listening PID 85515, got:\n%s", joined)
	}
	if !strings.Contains(joined, "/Users/hunter/.local/share/bastion/dolt/databases") {
		t.Errorf("external dump should show the live process data dir, got:\n%s", joined)
	}
	if !strings.Contains(joined, "EXTERNAL") {
		t.Errorf("external dump should flag the server as external, got:\n%s", joined)
	}
	// The stale gt-managed data dir must NOT be presented as the live data dir.
	// (config.DataDir may appear only inside the "gt's ... holds no databases"
	// note, which is not emitted when a real data dir was resolved.)
	if strings.Contains(joined, "  Data dir:   /Users/hunter/gt/.dolt-data") {
		t.Errorf("external dump must not show the empty shim as the live data dir, got:\n%s", joined)
	}
	// The frozen gt-managed log path must never be presented as the live log.
	if strings.Contains(joined, "Log file:   /Users/hunter/gt/daemon/dolt.log") {
		t.Errorf("external dump must not present the frozen gt-managed log as the live log, got:\n%s", joined)
	}
}

// TestDoltDumpTargetLinesExternalUnresolvedDataDir verifies the fallback when
// the live process --data-dir cannot be read: the output must still not present
// the empty shim as real, and must say the data dir is unknown.
func TestDoltDumpTargetLinesExternalUnresolvedDataDir(t *testing.T) {
	config := &doltserver.Config{
		Port:    3307,
		DataDir: "/Users/hunter/gt/.dolt-data",
		LogFile: "/Users/hunter/gt/daemon/dolt.log",
	}
	ext := doltserver.ExternalServerDiag{PID: 85515} // no DataDir/ConfigPath

	lines := doltDumpTargetLines(0, config, ext, true)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "unknown") {
		t.Errorf("external dump with no resolved data dir should say 'unknown', got:\n%s", joined)
	}
	if !strings.Contains(joined, "85515") {
		t.Errorf("external dump should still report the real PID, got:\n%s", joined)
	}
}
