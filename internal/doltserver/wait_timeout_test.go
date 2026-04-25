package doltserver

import (
	"testing"
)

// TestDefaultConfig_WaitTimeoutDefault verifies that the default config
// applies the gh-3623 idle-session timeout.
func TestDefaultConfig_WaitTimeoutDefault(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv("GT_DOLT_WAIT_TIMEOUT", "")

	config := DefaultConfig(townRoot)

	if config.WaitTimeoutSec != DefaultWaitTimeoutSec {
		t.Errorf("WaitTimeoutSec = %d, want %d", config.WaitTimeoutSec, DefaultWaitTimeoutSec)
	}
}

// TestDefaultConfig_WaitTimeoutEnvOverride verifies the GT_DOLT_WAIT_TIMEOUT
// env var raises or lowers the configured timeout.
func TestDefaultConfig_WaitTimeoutEnvOverride(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv("GT_DOLT_WAIT_TIMEOUT", "120")

	config := DefaultConfig(townRoot)

	if config.WaitTimeoutSec != 120 {
		t.Errorf("WaitTimeoutSec = %d, want 120", config.WaitTimeoutSec)
	}
}

// TestDefaultConfig_WaitTimeoutNegativeDisables verifies that a negative
// value opts out of the override, leaving Dolt's default in place.
func TestDefaultConfig_WaitTimeoutNegativeDisables(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv("GT_DOLT_WAIT_TIMEOUT", "-1")

	config := DefaultConfig(townRoot)

	if config.WaitTimeoutSec != 0 {
		t.Errorf("WaitTimeoutSec = %d, want 0 (disabled)", config.WaitTimeoutSec)
	}
}

// TestDefaultConfig_WaitTimeoutInvalidIgnored verifies that a non-numeric
// env value falls back to the default rather than zeroing the timeout.
func TestDefaultConfig_WaitTimeoutInvalidIgnored(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv("GT_DOLT_WAIT_TIMEOUT", "not-a-number")

	config := DefaultConfig(townRoot)

	if config.WaitTimeoutSec != DefaultWaitTimeoutSec {
		t.Errorf("WaitTimeoutSec = %d, want default %d when env var is invalid", config.WaitTimeoutSec, DefaultWaitTimeoutSec)
	}
}
