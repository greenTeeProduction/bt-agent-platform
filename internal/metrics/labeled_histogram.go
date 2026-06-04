package metrics

import "sync"

// LabeledHistogram tracks duration distributions per label set.
type LabeledHistogram struct {
	mu      sync.RWMutex
	bounds  []float64
	buckets map[string]*Histogram
}

// NewLabeledHistogram creates a histogram with per-label buckets.
func NewLabeledHistogram(bounds []float64) *LabeledHistogram {
	return &LabeledHistogram{
		bounds:  bounds,
		buckets: make(map[string]*Histogram),
	}
}

// Observe records a value for the given labels.
func (lh *LabeledHistogram) Observe(v float64, labels map[string]string) {
	key := labelKey(labels)
	lh.mu.RLock()
	h, ok := lh.buckets[key]
	lh.mu.RUnlock()
	if ok {
		h.Observe(v)
		return
	}
	lh.mu.Lock()
	defer lh.mu.Unlock()
	if h, ok = lh.buckets[key]; ok {
		h.Observe(v)
		return
	}
	h = NewHistogram(lh.bounds)
	h.Observe(v)
	lh.buckets[key] = h
}

// Snapshot returns sum and count per label key for export.
func (lh *LabeledHistogram) Snapshot() map[string]histogramSnap {
	lh.mu.RLock()
	defer lh.mu.RUnlock()
	out := make(map[string]histogramSnap, len(lh.buckets))
	for k, h := range lh.buckets {
		out[k] = h.SnapshotStats()
	}
	return out
}

type histogramSnap struct {
	Sum   float64
	Count uint64
}
