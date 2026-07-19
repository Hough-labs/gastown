package doltserver

import (
	"net"
	"os/exec"
	"strconv"
	"testing"
)

func TestDebugReapDiagnostics(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not available")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close free port listener: %v", err)
	}
	t.Setenv("GT_DOLT_PORT", strconv.Itoa(port))

	townRoot := t.TempDir()
	if err := Start(townRoot); err != nil {
		t.Fatalf("Start: %v", err)
	}
	running, pid, err := IsRunning(townRoot)
	t.Logf("running=%v pid=%d err=%v", running, pid, err)
	t.Cleanup(func() {
		_ = Stop(townRoot)
	})

	listeners := FindAllDoltListeners()
	t.Logf("listeners=%+v", listeners)
	for _, l := range listeners {
		t.Logf("PID=%d Port=%d isDoltSQLServerProcess=%v args=%v ppid=%d dataDir=%q established=%d",
			l.PID, l.Port, isDoltSQLServerProcess(l.PID), getProcessArgs(l.PID), getParentPID(l.PID), resolveDataDirFromProcess(l.PID), countEstablishedConnections(l.PID))
	}
}
