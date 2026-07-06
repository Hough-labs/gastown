package deacon

import (
	"os"
	"testing"
)

// TestMain installs a no-op heartbeat-label updater for the whole package so
// WriteHeartbeat-based tests run hermetically against a bare temp town, without
// shelling the real bd binary (which fails with "exit status 1" when the town
// has no provisioned beads DB for the heartbeat target). No deacon test
// exercises the real label update. (gfork-cpq)
func TestMain(m *testing.M) {
	SetHeartbeatLabelUpdaterForTest(func(string, *Heartbeat) error { return nil })
	os.Exit(m.Run())
}
