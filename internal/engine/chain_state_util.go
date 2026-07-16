package engine

// chainStateInt reads an integer cursor from ChainState, tolerating the
// float64 shape produced by JSON persistence round-trips (ADR-003/ADR-132).
func chainStateInt(bb *Blackboard, key string) (int, bool) {
	switch v := bb.ChainState[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
