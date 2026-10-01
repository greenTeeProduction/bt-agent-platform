package engine

import (
	"math"
	"strconv"
)

// chainStateInt reads an integer cursor from ChainState, tolerating the
// float64 shape produced by JSON persistence round-trips (ADR-003/ADR-132).
func chainStateInt(bb *Blackboard, key string) (int, bool) {
	switch v := bb.ChainState[key].(type) {
	case int:
		return v, true
	case int64:
		converted := int(v)
		return converted, int64(converted) == v
	case float64:
		// Reject fractional, non-finite and overflowing persisted cursors.
		// The upper bound is exclusive because MaxInt rounds up as float64.
		limit := math.Ldexp(1, strconv.IntSize-1)
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v < -limit || v >= limit {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}
