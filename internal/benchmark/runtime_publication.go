package benchmark

import (
	"context"
	"errors"
	"fmt"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

var ErrCandidateUnqualified = errors.New("candidate has not demonstrated task improvement")

// PublishRuntimeCandidate is shared by automated and operator-triggered evolution.
// Search scores never authorize publication. Only this fresh real-model paired
// comparison can supply the proof consumed by the immutable release store.
func PublishRuntimeCandidate(ctx context.Context, store *evolution.RuntimeReleaseStore, id, user string, base, candidate *evolution.SerializableNode, suite Suite) (*evolution.RuntimeQualification, *evolution.RuntimeRelease, error) {
	if store == nil || base == nil || candidate == nil {
		return nil, nil, fmt.Errorf("release store and both definitions required")
	}
	if info := engine.ValidateTreeFull(candidate); !info.Valid() {
		return nil, nil, fmt.Errorf("candidate validation: %v", info.Errors)
	}
	model, err := DefaultLLM()
	if err != nil {
		return nil, nil, err
	}
	live, ok := model.(*LiveModel)
	if !ok {
		return nil, nil, fmt.Errorf("publication requires the real benchmark provider")
	}
	q, err := QualifyRuntimeCandidate(ctx, id, user, base, candidate, suite, live)
	if err != nil {
		if recordErr := store.RecordAttempt(q, err); recordErr != nil {
			return q, nil, errors.Join(err, fmt.Errorf("retain rejected qualification: %w", recordErr))
		}
		return q, nil, fmt.Errorf("%w: %v", ErrCandidateUnqualified, err)
	}
	release, err := store.Promote(ctx, base, candidate, q)
	return q, release, err
}
