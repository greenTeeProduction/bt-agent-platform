package engine

// Status constants for behavior tree nodes.
// These extend the standard go-bt Status codes with Aborted.
const (
	StatusAborted int = -2 // Explicitly aborted by interrupt/event (Plan #3)
)
