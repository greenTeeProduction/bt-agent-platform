package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── validateNode edge cases ───

func TestValidateNode_UnknownAction(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "NonExistentAction12345",
	}
	missing := ValidateTree(tree)
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing, got %d: %v", len(missing), missing)
	}
	if missing[0] != "NonExistentAction12345" {
		t.Errorf("expected 'NonExistentAction12345', got %q", missing[0])
	}
}

func TestValidateNode_UnknownCondition(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Condition",
		Name: "NonExistentCondition67890",
	}
	missing := ValidateTree(tree)
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing, got %d: %v", len(missing), missing)
	}
	if missing[0] != "NonExistentCondition67890" {
		t.Errorf("expected 'NonExistentCondition67890', got %q", missing[0])
	}
}

func TestValidateNode_UnknownMixedTree(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "NonExistentAction"},
			{Type: "Condition", Name: "NonExistentCondition"},
			{Type: "Sequence", Name: "Nested", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "AnotherBadAction"},
			}},
		},
	}
	missing := ValidateTree(tree)
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing, got %d: %v", len(missing), missing)
	}
}

func TestValidateNode_SequenceSkip(t *testing.T) {
	// Sequence/Selector types should not add missing entries
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Seq",
	}
	missing := ValidateTree(tree)
	if len(missing) != 0 {
		t.Errorf("expected 0 missing for Sequence type, got %v", missing)
	}
}

// ─── walkValidate edge cases ───

func TestWalkValidate_NilNode(t *testing.T) {
	info := ValidateTreeFull(nil)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	foundNil := false
	for _, err := range info.Errors {
		if strings.Contains(err, "tree is nil") {
			foundNil = true
			break
		}
	}
	if !foundNil {
		t.Errorf("expected 'tree is nil' error, got: %v", info.Errors)
	}
}

func TestWalkValidate_RetryWithZeroMaxRetries(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Retry",
		Name:       "BadRetry",
		MaxRetries: 0,
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SetupDefaultTools"},
		},
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	foundRetry := false
	for _, err := range info.Errors {
		if strings.Contains(err, "requires max_retries") {
			foundRetry = true
			break
		}
	}
	if !foundRetry {
		t.Errorf("expected 'requires max_retries' error, got: %v", info.Errors)
	}
}

func TestWalkValidate_RepeaterWithZeroMaxRetries(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Repeater",
		Name:       "BadRepeater",
		MaxRetries: 0,
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "requires max_retries") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'requires max_retries' error, got: %v", info.Errors)
	}
}

func TestWalkValidate_DestroySideEffectNoGate(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "ExecutePlan",
		Metadata: map[string]any{
			"side_effect_class": "destroy",
		},
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "requires HumanApprovalGate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'requires HumanApprovalGate' error, got: %v", info.Errors)
	}
}

func TestWalkValidate_ExternalSideEffectNoGate(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "ExecutePlan",
		Metadata: map[string]any{
			"side_effect_class": "external",
		},
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "requires HumanApprovalGate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'requires HumanApprovalGate' error, got: %v", info.Errors)
	}
}

func TestWalkValidate_HumanApprovalGateSetsFlag(t *testing.T) {
	// Single HumanApprovalGate node — should set HasApprovalGate
	tree := &evolution.SerializableNode{
		Type: "HumanApprovalGate",
		Name: "Gate",
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if !info.HasApprovalGate {
		t.Errorf("expected HasApprovalGate to be true")
	}
}

func TestWalkValidate_DepthLimitExceeded(t *testing.T) {
	// Build a chain deep enough to exceed DefaultMaxDepth (16)
	root := &evolution.SerializableNode{Type: "Sequence", Name: "Depth0"}
	current := root
	for i := 1; i <= 20; i++ {
		child := &evolution.SerializableNode{Type: "Sequence", Name: "Depth"}
		current.Children = append(current.Children, *child)
		// Need to re-find the new child since we appended a copy
		current = &current.Children[len(current.Children)-1]
	}
	info := ValidateTreeFull(root)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "max depth") && strings.Contains(err, "exceeds limit") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected depth limit error, got: %v", info.Errors)
	}
}

func TestWalkValidate_NodeCountLimitExceeded(t *testing.T) {
	// Build tree exceeding DefaultMaxNodes (200)
	root := &evolution.SerializableNode{Type: "Sequence", Name: "Root"}
	for i := 0; i < 210; i++ {
		root.Children = append(root.Children, evolution.SerializableNode{
			Type: "Action",
			Name: "SetupDefaultTools",
		})
	}
	info := ValidateTreeFull(root)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.NodeCount < 200 {
		t.Fatalf("expected node count >200, got %d", info.NodeCount)
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "node count") && strings.Contains(err, "exceeds limit") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected node count limit error, got: %v", info.Errors)
	}
}

func TestWalkValidate_ParallelWidthLimitExceeded(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "RootPar",
	}
	for i := 0; i < 15; i++ {
		tree.Children = append(tree.Children, evolution.SerializableNode{
			Type: "Action",
			Name: "SetupDefaultTools",
		})
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "parallel width") && strings.Contains(err, "exceeds limit") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected parallel width limit error, got: %v", info.Errors)
	}
}

func TestWalkValidate_MaxRetriesLimitExceeded(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Retry",
		Name:       "TooManyRetries",
		MaxRetries: 10,
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	found := false
	for _, err := range info.Errors {
		if strings.Contains(err, "max retries") && strings.Contains(err, "exceeds limit") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected max retries limit error, got: %v", info.Errors)
	}
}

// ─── computeSubtreeMetrics edge cases ───

func TestComputeSubtreeMetrics_EmptyChildren(t *testing.T) {
	children := []evolution.SerializableNode{}
	depth, count, par, retries, timeout := computeSubtreeMetrics(children)
	if depth != 0 || count != 0 || par != 0 || retries != 0 || timeout != 0 {
		t.Errorf("expected all zeros for empty input, got depth=%d count=%d par=%d retries=%d timeout=%d",
			depth, count, par, retries, timeout)
	}
}

func TestComputeSubtreeMetrics_SingleChild(t *testing.T) {
	children := []evolution.SerializableNode{
		{Type: "Action", Name: "SetupDefaultTools"},
	}
	depth, count, par, retries, timeout := computeSubtreeMetrics(children)
	if depth != 0 {
		t.Errorf("expected depth 0, got %d", depth)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
	if par != 0 {
		t.Errorf("expected par 0, got %d", par)
	}
	if retries != 0 {
		t.Errorf("expected retries 0, got %d", retries)
	}
	if timeout != 0 {
		t.Errorf("expected timeout 0, got %d", timeout)
	}
}

// ─── sideEffectClassOfSubtree edge cases ───

func TestSideEffectClassOfSubtree_NoSideEffect(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SetupDefaultTools"},
			{Type: "Condition", Name: "HasClearTask"},
		},
	}
	sec := sideEffectClassOfSubtree(tree)
	if sec != "" {
		t.Errorf("expected empty side-effect class, got %q", sec)
	}
}

func TestSideEffectClassOfSubtree_DeepChild(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector",
				Name: "Middle",
				Children: []evolution.SerializableNode{
					{
						Type: "Action",
						Name: "ExecutePlan",
						Metadata: map[string]any{
							"side_effect_class": "database",
						},
					},
				},
			},
		},
	}
	sec := sideEffectClassOfSubtree(tree)
	if sec != "database" {
		t.Errorf("expected 'database' from deep child, got %q", sec)
	}
}

func TestSideEffectClassOfSubtree_MultipleChildren(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "RootPar",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{
				Type: "Action",
				Name: "B",
				Metadata: map[string]any{
					"side_effect_class": "filesystem",
				},
			},
			{Type: "Action", Name: "C"},
		},
	}
	sec := sideEffectClassOfSubtree(tree)
	if sec != "filesystem" {
		t.Errorf("expected 'filesystem' from child B, got %q", sec)
	}
}

func TestSideEffectClassOfSubtree_RootHasClass(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "test",
		Metadata: map[string]any{
			"side_effect_class": "network",
		},
	}
	sec := sideEffectClassOfSubtree(tree)
	if sec != "network" {
		t.Errorf("expected 'network', got %q", sec)
	}
}

// ─── computeTreeMetrics edge cases ───

func TestComputeTreeMetrics_NilNode(t *testing.T) {
	info := &evolution.NodeValidationInfo{}
	depth, count, par, retries, timeout, sec := computeTreeMetrics(nil, info)
	if depth != 0 || count != 0 || par != 0 || retries != 0 || timeout != 0 || sec != "" {
		t.Errorf("expected all zeros for nil node, got depth=%d count=%d par=%d retries=%d timeout=%d sec=%q",
			depth, count, par, retries, timeout, sec)
	}
}

func TestComputeTreeMetrics_ParallelWidthFromParent(t *testing.T) {
	// Parallel type with children - parent's parallel width should be set from len(Children)
	tree := &evolution.SerializableNode{
		Type: "Parallel",
		Name: "RootPar",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Action", Name: "B"},
		},
	}
	info := &evolution.NodeValidationInfo{}
	_, _, par, _, _, _ := computeTreeMetrics(tree, info)
	if par != 2 {
		t.Errorf("expected parallel width 2, got %d", par)
	}
}

func TestComputeTreeMetrics_ReactiveParallelWidth(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "ReactiveParallel",
		Name: "RootRP",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Action", Name: "B"},
			{Type: "Action", Name: "C"},
			{Type: "Action", Name: "D"},
		},
	}
	info := &evolution.NodeValidationInfo{}
	_, _, par, _, _, _ := computeTreeMetrics(tree, info)
	if par != 4 {
		t.Errorf("expected parallel width 4, got %d", par)
	}
}

// ─── walkValidate known node type edge ───

func TestWalkValidate_KnownNodeTypePasses(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "RootSeq",
		Children: []evolution.SerializableNode{
			{
				Type: "Action",
				Name: "SetupDefaultTools",
				Metadata: map[string]any{
					"side_effect_class": "none",
				},
			},
		},
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	// Unknown action error is expected (known action uses registry, not switch)
	// But we should NOT get "unknown node type"
	for _, err := range info.Errors {
		if strings.Contains(err, "unknown node type") {
			t.Errorf("unexpected 'unknown node type' error for Sequence: %s", err)
		}
	}
}

func TestWalkValidate_TimeoutPropagation(t *testing.T) {
	// Nested node with higher TimeoutMs should hit the if > path
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type:      "Action",
				Name:      "SetupDefaultTools",
				TimeoutMs: 30000,
			},
		},
	}
	info := ValidateTreeFull(tree)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.TimeoutMs != 30000 {
		t.Errorf("expected TimeoutMs 30000, got %d", info.TimeoutMs)
	}
}
