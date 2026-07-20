package crew

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// setupCrewManagerForTest builds a temp rig with a bare origin and returns a
// crew manager plus the rig path.
func setupCrewManagerForTest(t *testing.T) *Manager {
	t.Helper()
	tmpDir := t.TempDir()
	rigPath := filepath.Join(tmpDir, "test-rig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rig: %v", err)
	}
	bareRepoPath := filepath.Join(tmpDir, "bare-repo.git")
	if err := runCmd("git", "init", "--bare", bareRepoPath); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r := &rig.Rig{Name: "test-rig", Path: rigPath, GitURL: bareRepoPath}
	return NewManager(r, git.NewGit(rigPath))
}

func TestSetAutoRestartRoundTrips(t *testing.T) {
	mgr := setupCrewManagerForTest(t)
	if _, err := mgr.Add("iris", false); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Default is off.
	w, err := mgr.Get("iris")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if w.AutoRestart {
		t.Fatal("new crew should default to AutoRestart=false")
	}

	// Enable and confirm it persists through a fresh Get and List.
	if err := mgr.SetAutoRestart("iris", true); err != nil {
		t.Fatalf("SetAutoRestart(true): %v", err)
	}
	if w, err = mgr.Get("iris"); err != nil {
		t.Fatalf("Get after enable: %v", err)
	}
	if !w.AutoRestart {
		t.Fatal("AutoRestart should be true after enabling")
	}
	workers, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(workers) != 1 || !workers[0].AutoRestart {
		t.Fatalf("List did not reflect AutoRestart: %+v", workers)
	}

	// Disable and confirm.
	if err := mgr.SetAutoRestart("iris", false); err != nil {
		t.Fatalf("SetAutoRestart(false): %v", err)
	}
	if w, err = mgr.Get("iris"); err != nil {
		t.Fatalf("Get after disable: %v", err)
	}
	if w.AutoRestart {
		t.Fatal("AutoRestart should be false after disabling")
	}
}

func TestSetAutoRestartUnknownCrew(t *testing.T) {
	mgr := setupCrewManagerForTest(t)
	if err := mgr.SetAutoRestart("ghost", true); err == nil {
		t.Fatal("expected error setting auto-restart on a non-existent crew")
	}
}
