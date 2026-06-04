package engine

// ParallelMode is the execution mode for ReactiveParallel nodes.
type ParallelMode int

const (
	ParallelAll     ParallelMode = iota // Success when ALL succeed
	ParallelAny                         // Success when any ONE succeeds
	ParallelRace                        // First terminal result wins
	ParallelMonitor                     // Monitor children can cancel actions
)

// valAsFloat64 converts various value types to float64 with ok flag.
// Handles float64, int, int64, bool (1.0/0.0), and string "true"/"1.0".
func valAsFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case bool:
		if val {
			return 1.0, true
		}
		return 0.0, true
	case string:
		if val == "true" || val == "1.0" || val == "1" {
			return 1.0, true
		}
		if val == "false" || val == "0.0" || val == "0" {
			return 0.0, true
		}
		return 0.0, false
	default:
		return 0.0, false
	}
}
