package witness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldReapUnderPressure(t *testing.T) {
	pressured := slotOpenSchedulerStatus{QueuedReady: 3}
	pressured.Capacity.Max = 11
	pressured.Capacity.Free = 0

	cases := []struct {
		name   string
		mutate func(*slotOpenSchedulerStatus)
		want   bool
	}{
		{"starved-with-queue", func(*slotOpenSchedulerStatus) {}, true},
		{"paused", func(s *slotOpenSchedulerStatus) { s.Paused = true }, false},
		{"not-capacity-mode", func(s *slotOpenSchedulerStatus) { s.Capacity.Max = 0 }, false},
		{"free-slots", func(s *slotOpenSchedulerStatus) { s.Capacity.Free = 2 }, false},
		{"empty-queue", func(s *slotOpenSchedulerStatus) { s.QueuedReady = 0 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := pressured
			tc.mutate(&s)
			got, reason := shouldReapUnderPressure(s)
			if got != tc.want {
				t.Fatalf("shouldReapUnderPressure = %v (%q), want %v", got, reason, tc.want)
			}
			if !got && reason == "" {
				t.Fatal("expected a non-empty skip reason on no-op")
			}
		})
	}
}

// withReapSeams swaps the reap injection seams for a test and restores them.
func withReapSeams(t *testing.T, status slotOpenSchedulerStatus, statusErr error, safe func(string) bool, nuked *[]string) {
	t.Helper()
	origStatus, origSafe, origNuke := reapReadSchedulerStatus, reapPolecatSafeToNuke, reapNukePolecat
	t.Cleanup(func() {
		reapReadSchedulerStatus = origStatus
		reapPolecatSafeToNuke = origSafe
		reapNukePolecat = origNuke
	})
	reapReadSchedulerStatus = func(string) (slotOpenSchedulerStatus, error) { return status, statusErr }
	reapPolecatSafeToNuke = func(_, _, polecatName string) (bool, string) {
		if safe(polecatName) {
			return true, ""
		}
		return false, "check-recovery verdict=NEEDS_RECOVERY"
	}
	reapNukePolecat = func(_ *BdCli, _, _, polecatName string) error {
		*nuked = append(*nuked, polecatName)
		return nil
	}
}

func makePolecatDirs(t *testing.T, townRoot, rigName string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(townRoot, rigName, "polecats", n), 0755); err != nil {
			t.Fatalf("mkdir polecat %s: %v", n, err)
		}
	}
}

func pressuredStatus(queuedReady int) slotOpenSchedulerStatus {
	s := slotOpenSchedulerStatus{QueuedReady: queuedReady}
	s.Capacity.Max = 11
	s.Capacity.Free = 0
	return s
}

func TestReapSafeToNukeUnderPressureReapsBoundedByQueue(t *testing.T) {
	townRoot := t.TempDir()
	makePolecatDirs(t, townRoot, "feryn", "chrome", "guzzle", "nitro")

	var nuked []string
	// All three classify SAFE_TO_NUKE, but only 2 beads are queued-ready.
	withReapSeams(t, pressuredStatus(2), nil, func(string) bool { return true }, &nuked)

	result := ReapSafeToNukeUnderPressure(nil, townRoot, "feryn")
	if len(result.Reaped) != 2 {
		t.Fatalf("reaped %v, want exactly 2 (bounded by queued_ready)", result.Reaped)
	}
	if len(nuked) != 2 {
		t.Fatalf("NukePolecat called %d times, want 2", len(nuked))
	}
}

func TestReapSafeToNukeUnderPressureSkipsNonSafe(t *testing.T) {
	townRoot := t.TempDir()
	makePolecatDirs(t, townRoot, "feryn", "chrome", "guzzle")

	var nuked []string
	// Only "chrome" is SAFE_TO_NUKE; "guzzle" still has recoverable work.
	withReapSeams(t, pressuredStatus(5), nil, func(name string) bool { return name == "chrome" }, &nuked)

	result := ReapSafeToNukeUnderPressure(nil, townRoot, "feryn")
	if len(result.Reaped) != 1 || result.Reaped[0] != "chrome" {
		t.Fatalf("reaped %v, want [chrome] only", result.Reaped)
	}
	if len(nuked) != 1 || nuked[0] != "chrome" {
		t.Fatalf("nuked %v, want [chrome] — a recoverable polecat must never be reaped", nuked)
	}
}

func TestReapSafeToNukeUnderPressureNoOpWithoutPressure(t *testing.T) {
	townRoot := t.TempDir()
	makePolecatDirs(t, townRoot, "feryn", "chrome")

	var nuked []string
	// Free capacity available → preserve reusable-idle polecats, reap nothing.
	free := pressuredStatus(5)
	free.Capacity.Free = 3
	withReapSeams(t, free, nil, func(string) bool { return true }, &nuked)

	result := ReapSafeToNukeUnderPressure(nil, townRoot, "feryn")
	if len(result.Reaped) != 0 || len(nuked) != 0 {
		t.Fatalf("reaped %v (nuked %v), want none when capacity is available", result.Reaped, nuked)
	}
	if result.Skipped == "" {
		t.Fatal("expected a skip reason when not under pressure")
	}
}

func TestReapSafeToNukeUnderPressureRespectsKillSwitch(t *testing.T) {
	t.Setenv(disableAutoReapEnv, "1")
	townRoot := t.TempDir()
	makePolecatDirs(t, townRoot, "feryn", "chrome")

	var nuked []string
	withReapSeams(t, pressuredStatus(5), nil, func(string) bool { return true }, &nuked)

	result := ReapSafeToNukeUnderPressure(nil, townRoot, "feryn")
	if len(nuked) != 0 {
		t.Fatalf("kill switch set but nuked %v", nuked)
	}
	if result.Skipped == "" {
		t.Fatal("expected a skip reason when disabled via kill switch")
	}
}
