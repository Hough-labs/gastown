//go:build integration

package cmd

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/steveyegge/gastown/internal/doltserver"
	"github.com/steveyegge/gastown/internal/testutil"
)

func TestMain(m *testing.M) {
	// Force sequential test execution to avoid bd file locks on Windows.
	_ = flag.Set("test.parallel", "1")
	flag.Parse()

	// Crash-safe cleanup: reap any embedded dolt sql-server processes orphaned
	// by a previous run of this package crashing before its own t.Cleanup ran
	// (gfork-d5h). Scoped to orphaned/ephemeral servers only — see
	// doltserver.ReapOrphanedDoltProcesses; never touches the shared
	// bastion/production server.
	if stopped, err := doltserver.ReapOrphanedDoltProcesses(); err != nil {
		fmt.Fprintf(os.Stderr, "integration TestMain: pre-run orphan reap: %v\n", err)
	} else if stopped > 0 {
		fmt.Fprintf(os.Stderr, "integration TestMain: reaped %d orphaned Dolt process(es) from a prior run\n", stopped)
	}

	// Start an ephemeral Dolt container for this package's integration tests.
	// Tests like TestAgentWorktreesStayClean and TestBeadsRoutingFromTownRoot
	// spawn gt/bd subprocesses that create databases (e.g., "tr", "hq").
	// By routing to an isolated container (via GT_DOLT_PORT), those databases
	// are destroyed when the container is terminated at cleanup —
	// preventing orphan accumulation in the shared production Dolt data dir.
	if err := testutil.EnsureDoltContainerForTestMain(); err != nil {
		fmt.Fprintf(os.Stderr, "integration TestMain: dolt setup: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Clean up the shared Dolt container.
	testutil.TerminateDoltContainer()
	os.Exit(code)
}
