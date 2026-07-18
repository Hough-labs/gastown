package capacity

import "testing"

func intPtr(n int) *int { return &n }

func TestGetReviewerReserve(t *testing.T) {
	tests := []struct {
		name string
		cfg  *SchedulerConfig
		want int
	}{
		{"nil config", nil, 0},
		{"nil field", &SchedulerConfig{}, 0},
		{"zero", &SchedulerConfig{ReviewerReserve: intPtr(0)}, 0},
		{"positive", &SchedulerConfig{ReviewerReserve: intPtr(3)}, 3},
		{"negative clamps to zero", &SchedulerConfig{ReviewerReserve: intPtr(-2)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetReviewerReserve(); got != tt.want {
				t.Errorf("GetReviewerReserve() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEffectiveReviewerReserve(t *testing.T) {
	tests := []struct {
		name    string
		reserve *int
		max     int
		want    int
	}{
		{"direct dispatch (max -1) yields 0", intPtr(2), -1, 0},
		{"disabled (max 0) yields 0", intPtr(2), 0, 0},
		{"within bounds", intPtr(2), 8, 2},
		{"zero reserve", intPtr(0), 8, 0},
		{"clamped to max-1 to keep a worker slot", intPtr(8), 8, 7},
		{"reserve above max clamps to max-1", intPtr(20), 8, 7},
		{"max of 1 forces reserve 0", intPtr(1), 1, 0},
		{"nil reserve yields 0", nil, 8, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SchedulerConfig{ReviewerReserve: tt.reserve}
			if got := cfg.EffectiveReviewerReserve(tt.max); got != tt.want {
				t.Errorf("EffectiveReviewerReserve(%d) = %d, want %d", tt.max, got, tt.want)
			}
		})
	}
}

func TestDefaultSchedulerConfig_ReviewerReserveZero(t *testing.T) {
	cfg := DefaultSchedulerConfig()
	if got := cfg.GetReviewerReserve(); got != 0 {
		t.Errorf("default reviewer reserve = %d, want 0 (opt-in, no behavior change)", got)
	}
}
