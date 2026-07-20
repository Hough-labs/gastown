package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestDescribeDispatchStatus(t *testing.T) {
	// Pressure-disabled daemon config (the default: all thresholds 0).
	noPressure := &config.DaemonThresholds{}

	cpu := 2.0
	withPressure := &config.DaemonThresholds{PressureCPUThreshold: &cpu}

	tests := []struct {
		name       string
		paused     bool
		readyCount int
		snap       polecatCapacitySnapshot
		cfg        *config.DaemonThresholds
		want       string // "" means expect empty; otherwise a substring that must appear
		wantEmpty  bool
	}{
		{
			name:       "no ready work is silent",
			readyCount: 0,
			snap:       polecatCapacitySnapshot{Max: 11, Free: 10},
			cfg:        noPressure,
			wantEmpty:  true,
		},
		{
			name:       "paused holds dispatch",
			paused:     true,
			readyCount: 2,
			snap:       polecatCapacitySnapshot{Max: 11, Free: 10},
			cfg:        noPressure,
			want:       "paused",
		},
		{
			name:       "direct-dispatch mode when max_polecats is zero",
			readyCount: 1,
			snap:       polecatCapacitySnapshot{Max: 0, Free: 0},
			cfg:        noPressure,
			want:       "direct-dispatch",
		},
		{
			name:       "no free capacity is held, not stuck",
			readyCount: 3,
			snap:       polecatCapacitySnapshot{Max: 11, Free: 0},
			cfg:        noPressure,
			want:       "no free capacity",
		},
		{
			// The phantom-wedge case: ready work + idle capacity. Must explain
			// the heartbeat cadence rather than look stuck.
			name:       "ready with capacity explains heartbeat cadence",
			readyCount: 1,
			snap:       polecatCapacitySnapshot{Max: 11, Free: 10},
			cfg:        noPressure,
			want:       "next daemon heartbeat",
		},
		{
			name:       "pressure note only when pressure gating configured",
			readyCount: 1,
			snap:       polecatCapacitySnapshot{Max: 11, Free: 10},
			cfg:        withPressure,
			want:       "pressure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeDispatchStatus(tt.paused, tt.readyCount, tt.snap, tt.cfg)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty status, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("status %q does not contain %q", got, tt.want)
			}
		})
	}
}

func TestDescribeDispatchStatus_NoPressureNoteWhenDisabled(t *testing.T) {
	got := describeDispatchStatus(false, 1, polecatCapacitySnapshot{Max: 11, Free: 10}, &config.DaemonThresholds{})
	if strings.Contains(got, "pressure") {
		t.Fatalf("pressure note should be absent when gating disabled, got %q", got)
	}
}
