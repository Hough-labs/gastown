// Package capacity provides types and pure functions for the capacity-controlled
// dispatch scheduler. The impure orchestration (dispatch loop, enqueue, epic/convoy
// resolution) stays in cmd but uses types and pure functions from this package.
package capacity

import "time"

// SchedulerConfig configures the capacity scheduler for polecat dispatch.
// This is a town-wide setting (not per-rig) because capacity control is host-wide:
// API rate limits, memory, and CPU are shared resources across all rigs.
//
// Behavior is driven entirely by MaxPolecats:
//   -1 (default): direct dispatch — gt sling works as before, near-zero overhead
//    0:           direct dispatch (same as -1)
//    N > 0:       deferred dispatch — labels/metadata applied, daemon dispatches
type SchedulerConfig struct {
	// MaxPolecats is the max concurrent polecats across ALL rigs.
	// Includes both scheduler-dispatched and directly-slung polecats.
	// nil/absent = default (-1, direct dispatch). 0 = direct dispatch (same as -1).
	// N > 0 = deferred dispatch with capacity control.
	MaxPolecats *int `json:"max_polecats,omitempty"`

	// BatchSize is the number of beads to dispatch per heartbeat tick.
	// Limits spawn rate per 3-minute cycle.
	// nil/absent = default (1). Explicit 0 is rejected by config setter.
	BatchSize *int `json:"batch_size,omitempty"`

	// SpawnDelay is the delay between spawns to prevent Dolt lock contention.
	// Default: "0s".
	SpawnDelay string `json:"spawn_delay,omitempty"`

	// ReviewerReserve is the number of slots (out of MaxPolecats) reserved for
	// reviewer polecats. Worker polecats may occupy at most (MaxPolecats -
	// ReviewerReserve) slots; reviewer polecats may use the full pool. This
	// prevents a burst of worker dispatches from starving per-PR reviewer spawn
	// and deadlocking the merge queue (hq-2b2v).
	// nil/absent = default (0, no reserve — shared pool, prior behavior).
	ReviewerReserve *int `json:"reviewer_reserve,omitempty"`
}

// DefaultSchedulerConfig returns a SchedulerConfig with sensible defaults.
// MaxPolecats=-1 means direct dispatch (no scheduler overhead).
func DefaultSchedulerConfig() *SchedulerConfig {
	defaultMax := -1
	defaultBatch := 1
	defaultReserve := 0
	return &SchedulerConfig{
		MaxPolecats:     &defaultMax,
		BatchSize:       &defaultBatch,
		SpawnDelay:      "0s",
		ReviewerReserve: &defaultReserve,
	}
}

// GetMaxPolecats returns MaxPolecats or the default (-1, direct dispatch) if unset.
func (c *SchedulerConfig) GetMaxPolecats() int {
	if c == nil || c.MaxPolecats == nil {
		return -1
	}
	return *c.MaxPolecats
}

// GetBatchSize returns BatchSize or the default (1) if unset.
func (c *SchedulerConfig) GetBatchSize() int {
	if c == nil || c.BatchSize == nil {
		return 1
	}
	return *c.BatchSize
}

// GetSpawnDelay returns SpawnDelay as a duration, defaulting to 0s.
func (c *SchedulerConfig) GetSpawnDelay() time.Duration {
	if c == nil || c.SpawnDelay == "" {
		return 0
	}
	return ParseDurationOrDefault(c.SpawnDelay, 0)
}

// IsDeferred returns true when the scheduler is configured for deferred dispatch
// (max_polecats > 0). Returns false for direct dispatch (-1) and disabled (0).
func (c *SchedulerConfig) IsDeferred() bool {
	return c.GetMaxPolecats() > 0
}

// GetReviewerReserve returns the configured reviewer reserve or the default (0)
// if unset. This is the raw configured value; use EffectiveReviewerReserve to
// clamp it against the pool size before applying admission math.
func (c *SchedulerConfig) GetReviewerReserve() int {
	if c == nil || c.ReviewerReserve == nil {
		return 0
	}
	if *c.ReviewerReserve < 0 {
		return 0
	}
	return *c.ReviewerReserve
}

// EffectiveReviewerReserve returns the reviewer reserve clamped against a pool
// of the given size. When the pool is unbounded/direct (max <= 0) the reserve
// is meaningless and returns 0. Otherwise the reserve is clamped to [0, max-1]
// so that at least one slot always remains available to worker polecats — a
// reserve that swallowed the entire pool would deadlock worker dispatch.
func (c *SchedulerConfig) EffectiveReviewerReserve(max int) int {
	if max <= 0 {
		return 0
	}
	reserve := c.GetReviewerReserve()
	if reserve > max-1 {
		reserve = max - 1
	}
	return reserve
}

// ParseDurationOrDefault parses a Go duration string, returning fallback on error or empty input.
func ParseDurationOrDefault(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
