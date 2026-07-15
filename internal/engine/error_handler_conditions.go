package engine

import "strings"

// Name-parameterized guard conditions for Claude-proposed recovery nodes,
// mirroring the compiled-GOAP "GoapStateMatches:k=v" pattern
// (goap_compiled_nodes.go). They read the classified error state that
// recordNodeFailure (reliability_decorators.go) stores on the blackboard.
const (
	errorCategoryCondPrefix = "LastErrorCategoryIs:"
	errorNodeCondPrefix     = "LastErrorNodeIs:"
)

func errorHandlerConditionFor(name string) ConditionFunc {
	chainStateEquals := func(key, want string) ConditionFunc {
		return func(b *Blackboard) bool {
			if want == "" || b == nil || b.ChainState == nil {
				return false // malformed spec must not silently pass
			}
			got, _ := b.ChainState[key].(string)
			return got == want
		}
	}
	switch {
	case strings.HasPrefix(name, errorCategoryCondPrefix):
		return chainStateEquals("last_error_category", strings.TrimSpace(strings.TrimPrefix(name, errorCategoryCondPrefix)))
	case strings.HasPrefix(name, errorNodeCondPrefix):
		return chainStateEquals("last_error_node", strings.TrimSpace(strings.TrimPrefix(name, errorNodeCondPrefix)))
	}
	return nil
}
