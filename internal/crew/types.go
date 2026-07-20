// Package crew provides crew workspace management for overseer workspaces.
package crew

import "time"

// CrewWorker represents a user-managed workspace in a rig.
type CrewWorker struct {
	// Name is the crew worker identifier.
	Name string `json:"name"`

	// Rig is the rig this crew worker belongs to.
	Rig string `json:"rig"`

	// ClonePath is the path to the crew worker's clone of the rig.
	ClonePath string `json:"clone_path"`

	// Branch is the current git branch.
	Branch string `json:"branch"`

	// CreatedAt is when the crew worker was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the crew worker was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// AutoRestart opts this crew worker into daemon liveness supervision: when
	// true, the daemon restarts the crew if its session dies while the rig is
	// operational. Off by default so the daemon never fights a deliberate
	// `gt crew stop`; enable per-crew via `gt crew autorestart <name> on`
	// (feryn-409i).
	AutoRestart bool `json:"auto_restart,omitempty"`
}

// Summary provides a concise view of crew worker status.
type Summary struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
}

// Summary returns a Summary for this crew worker.
func (c *CrewWorker) Summary() Summary {
	return Summary{
		Name:   c.Name,
		Branch: c.Branch,
	}
}
