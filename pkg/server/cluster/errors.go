package cluster

import "errors"

// Sentinel errors for the cluster package.
var (
	// ErrNodeNotFound is returned when a node lookup finds no matching entry.
	ErrNodeNotFound = errors.New("cluster: node not found")

	// ErrSlotConflict is returned when a node tries to register a
	// DataCenterID+MachineID pair that is already held by a different running node.
	ErrSlotConflict = errors.New("cluster: DataCenterID+MachineID slot already taken")

	// ErrMigrationInProgress is returned when Begin is called while another
	// provider migration is already underway.
	ErrMigrationInProgress = errors.New("cluster: provider migration already in progress")

	// ErrNotStarted is returned when an operation is attempted before the
	// cluster provider has been started.
	ErrNotStarted = errors.New("cluster: provider not started")

	// ErrEmptyCandidates is returned by the load balancer when the filtered
	// candidate list is empty.
	ErrEmptyCandidates = errors.New("cluster: no candidates after filtering")
)
