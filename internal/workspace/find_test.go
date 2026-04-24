package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("realpath: %v", err)
	}
	return real
}

func TestFindWithPrimaryMarker(t *testing.T) {
	// Create temp workspace structure
	root := realPath(t, t.TempDir())
	mayorDir := filepath.Join(root, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"town"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create nested directory
	nested := filepath.Join(root, "some", "deep", "path")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// Find from nested should return root
	found, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindWithSecondaryMarker(t *testing.T) {
	// Create temp workspace with just mayor/ directory
	root := realPath(t, t.TempDir())
	mayorDir := filepath.Join(root, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create nested directory
	nested := filepath.Join(root, "rigs", "test")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	// Find from nested should return root
	found, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindNotFound(t *testing.T) {
	// Create temp dir with no markers
	dir := t.TempDir()

	found, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != "" {
		t.Errorf("Find = %q, want empty string", found)
	}
}

func TestFindOrErrorNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := FindOrError(dir)
	if err != ErrNotFound {
		t.Errorf("FindOrError = %v, want ErrNotFound", err)
	}
}

func TestFindAtRoot(t *testing.T) {
	// Create workspace at temp root level
	root := realPath(t, t.TempDir())
	mayorDir := filepath.Join(root, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"town"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Find from root should return root
	found, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestIsWorkspace(t *testing.T) {
	root := t.TempDir()

	// Not a workspace initially
	is, err := IsWorkspace(root)
	if err != nil {
		t.Fatalf("IsWorkspace: %v", err)
	}
	if is {
		t.Error("expected not a workspace initially")
	}

	// Add primary marker (mayor/town.json)
	mayorDir := filepath.Join(root, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"town"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Now is a workspace
	is, err = IsWorkspace(root)
	if err != nil {
		t.Fatalf("IsWorkspace: %v", err)
	}
	if !is {
		t.Error("expected to be a workspace")
	}
}

func TestFindFromSymlinkedDir(t *testing.T) {
	root := realPath(t, t.TempDir())
	mayorDir := filepath.Join(root, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townFile, []byte(`{"type":"town"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	linkTarget := filepath.Join(root, "actual")
	if err := os.MkdirAll(linkTarget, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	linkName := filepath.Join(root, "linked")
	if err := os.Symlink(linkTarget, linkName); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	found, err := Find(linkName)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != root {
		t.Errorf("Find = %q, want %q", found, root)
	}
}

func TestFindPreservesSymlinkPath(t *testing.T) {
	realRoot := t.TempDir()
	resolved, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	symRoot := filepath.Join(t.TempDir(), "symlink-workspace")
	if err := os.Symlink(resolved, symRoot); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	mayorDir := filepath.Join(symRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	townFile := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townFile, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	subdir := filepath.Join(symRoot, "rigs", "project", "polecats", "worker")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	townRoot, err := Find(subdir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if townRoot != symRoot {
		t.Errorf("Find returned %q, want %q (symlink path preserved)", townRoot, symRoot)
	}

	relPath, err := filepath.Rel(townRoot, subdir)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}

	if filepath.ToSlash(relPath) != "rigs/project/polecats/worker" {
		t.Errorf("Rel = %q, want 'rigs/project/polecats/worker'", relPath)
	}
}

func TestFindSkipsNestedWorkspaceInWorktree(t *testing.T) {
	root := realPath(t, t.TempDir())

	if err := os.MkdirAll(filepath.Join(root, "mayor"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "mayor", "town.json"), []byte(`{"name":"outer"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	polecatDir := filepath.Join(root, "myrig", "polecats", "worker")
	if err := os.MkdirAll(filepath.Join(polecatDir, "mayor"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(polecatDir, "mayor", "town.json"), []byte(`{"name":"inner"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	found, err := Find(polecatDir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if found != root {
		t.Errorf("Find = %q, want %q (should skip nested workspace in polecats/)", found, root)
	}

	rel, _ := filepath.Rel(found, polecatDir)
	if filepath.ToSlash(rel) != "myrig/polecats/worker" {
		t.Errorf("Rel = %q, want 'myrig/polecats/worker'", rel)
	}
}

func TestFindSkipsNestedWorkspaceInCrew(t *testing.T) {
	root := realPath(t, t.TempDir())

	if err := os.MkdirAll(filepath.Join(root, "mayor"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "mayor", "town.json"), []byte(`{"name":"outer"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	crewDir := filepath.Join(root, "myrig", "crew", "worker")
	if err := os.MkdirAll(filepath.Join(crewDir, "mayor"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(crewDir, "mayor", "town.json"), []byte(`{"name":"inner"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	found, err := Find(crewDir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if found != root {
		t.Errorf("Find = %q, want %q (should skip nested workspace in crew/)", found, root)
	}
}

// TestFindFromCwdOrError_EnvWinsOverPhantomCwd regression-guards the
// phantom-town incident: CWD happens to contain a mayor/ subdir that
// isn't the agent's real workspace. With env-first precedence
// GT_TOWN_ROOT must win. Otherwise the agent's next `gt daemon run`
// would acquire a lock in the phantom and start a parallel daemon.
func TestFindFromCwdOrError_EnvWinsOverPhantomCwd(t *testing.T) {
	tmp := realPath(t, t.TempDir())

	realTown := filepath.Join(tmp, "real")
	phantom := filepath.Join(tmp, "phantom")
	for _, root := range []string{realTown, phantom} {
		if err := os.MkdirAll(filepath.Join(root, "mayor"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", root, err)
		}
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(phantom); err != nil {
		t.Fatalf("chdir phantom: %v", err)
	}
	t.Setenv("GT_TOWN_ROOT", realTown)
	t.Setenv("GT_ROOT", "")

	got, err := FindFromCwdOrError()
	if err != nil {
		t.Fatalf("FindFromCwdOrError: %v", err)
	}
	if got != realTown {
		t.Errorf("got %q, want %q (env-set town must win over CWD-derived phantom)", got, realTown)
	}
}

// TestFindFromCwdOrError_FallsBackToCwdWhenEnvInvalid confirms the env-first
// branch degrades gracefully: env set to a non-workspace path falls through
// to the CWD walk.
func TestFindFromCwdOrError_FallsBackToCwdWhenEnvInvalid(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	town := filepath.Join(tmp, "town")
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatalf("mkdir town: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(town); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Setenv("GT_TOWN_ROOT", filepath.Join(tmp, "does-not-exist"))
	t.Setenv("GT_ROOT", "")

	got, err := FindFromCwdOrError()
	if err != nil {
		t.Fatalf("FindFromCwdOrError: %v", err)
	}
	if got != town {
		t.Errorf("got %q, want %q (invalid env must fall through to CWD walk)", got, town)
	}
}
