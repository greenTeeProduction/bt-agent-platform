package evolution

import (
	"encoding/json"
	"slices"
	"strings"
)

// GovernanceAssessment describes controls attached to actual task work. It is
// a structural selection signal, not evidence that a task or a gate succeeded.
// Counts saturate per work node: duplicate gates and decorative depth earn no
// credit. A recovery-only tree has no task work and receives zero credit.
type GovernanceAssessment struct {
	WorkNodes       int     `json:"work_nodes"`
	InputGuarded    int     `json:"input_guarded"`
	ResultChecked   int     `json:"result_checked"`
	ContractChecked int     `json:"contract_checked"`
	Guided          int     `json:"guided"`
	Bounded         int     `json:"bounded"`
	Recoverable     int     `json:"recoverable"`
	Score           float64 `json:"score"`
	controls        map[string][]governanceControl
}

type governanceControl struct {
	flags     uint8
	contracts []string
}

type governanceContext struct {
	contractChecked                                bool
	contracts                                      []string
	guarded, checked, bounded, recoverable, masked bool
}

// AssessGovernance credits executable controls in their control-flow context.
// A sibling gate on an alternative Selector branch does not protect a worker.
// QualityGate and declared CheckpointVerifier contracts protect their child;
// input guards must precede work on a mandatory sequence path.
func AssessGovernance(tree *SerializableNode) GovernanceAssessment {
	var report GovernanceAssessment
	report.controls = make(map[string][]governanceControl)
	var visit func(*SerializableNode, governanceContext)
	visit = func(n *SerializableNode, ctx governanceContext) {
		if n == nil {
			return
		}
		ctx.bounded = ctx.bounded || (n.Type == "Timeout" && len(n.Children) > 0) || hasExecutionBudget(n) || boundedRecovery(n)
		if n.Type == "Succeeder" || n.Type == "Inverter" {
			ctx.masked = true
		}
		if IsTaskWork(n) {
			var controls uint8
			report.WorkNodes++
			if ctx.guarded {
				report.InputGuarded++
				controls |= 1
			}
			if ctx.checked && !ctx.masked {
				report.ResultChecked++
				controls |= 2
			}
			if hasAgentGuidance(n) {
				report.Guided++
				controls |= 4
			}
			if ctx.bounded {
				report.Bounded++
				controls |= 8
			}
			if ctx.recoverable {
				report.Recoverable++
				controls |= 16
			}
			key := n.Type + ":" + n.Name
			required := slices.Clone(ctx.contracts)
			if ctx.masked || !ctx.checked {
				required = nil
			}
			if len(required) > 0 && ctx.contractChecked {
				report.ContractChecked++
			}
			report.controls[key] = append(report.controls[key], governanceControl{flags: controls, contracts: required})
			return // Action nodes cannot execute decorative children.
		}
		switch n.Type {
		case "QualityGate", "FileTask":
			if contract, err := ParseResultContract(n); err == nil && contract != nil {
				data, _ := json.Marshal(contract)
				ctx.contracts = append(slices.Clone(ctx.contracts), string(data))
				ctx.contractChecked = ctx.contractChecked || len(contract.JSONFields) > 0 || len(contract.RequiredKeys) > 0
			}
			ctx.checked = ctx.checked || len(n.Children) > 0
			if len(n.Children) > 1 && (boundedRecovery(&n.Children[1]) || (n.Children[1].Type == "ChainAction" && strings.HasPrefix(n.Children[1].Name, "llm_call:") && hasExecutionBudget(&n.Children[1]))) {
				ctx.recoverable = true
			}
		case "CheckpointVerifier":
			if contract, err := ParseCheckpointContract(n); err == nil {
				data, _ := json.Marshal(contract)
				ctx.contracts = append(slices.Clone(ctx.contracts), "checkpoint:"+string(data))
				ctx.checked = true
			}
		case "HumanApprovalGate":
			ctx.guarded = true
		}
		children := executableChildren(n)
		sequence := n.Type == "Sequence" || n.Type == "MemSequence" || n.Type == "PersistentMemSequence"
		for i := range children {
			recoveryOnly, hasQualityEdge := false, false
			for _, edge := range n.Edges {
				if edge.Type == EdgeRecovery && edge.ChildIndex == i {
					recoveryOnly = true
				}
				if edge.Type == EdgeQualityGate && edge.ChildIndex >= 0 && edge.ChildIndex < len(n.Children) {
					hasQualityEdge = true
				}
			}
			if recoveryOnly && !hasQualityEdge {
				continue
			}
			childCtx := ctx
			if sequence {
				// An enclosing output gate only sees the last worker result.
				for j := i + 1; j < len(n.Children); j++ {
					if subtreeHasTaskWork(&n.Children[j]) {
						childCtx.checked = false
						childCtx.contracts = nil
						childCtx.contractChecked = false
						break
					}
				}
				for j := i + 1; j < len(n.Children); j++ {
					if !optionalEdge(n, j) && isMandatoryResultCheck(&n.Children[j]) {
						childCtx.checked = true
						break
					}
					if subtreeHasTaskWork(&n.Children[j]) {
						// A later worker may replace the result: its final check
						// is not evidence of an intermediate check on this step.
						break
					}
				}
			}
			for _, edge := range n.Edges {
				if edge.ChildIndex == i && (edge.Type == EdgeQualityGate || edge.Type == EdgeRecovery) {
					childCtx.checked = true
				}
			}
			visit(&n.Children[i], childCtx)
			if sequence && !optionalEdge(n, i) && isMandatoryInputGuard(&n.Children[i]) {
				ctx.guarded = true
			}
		}
	}
	visit(tree, governanceContext{})
	if report.WorkNodes > 0 {
		n := float64(report.WorkNodes)
		report.Score = (25*float64(report.InputGuarded) + 25*float64(report.ResultChecked) + 10*float64(report.ContractChecked) +
			20*float64(report.Guided) + 15*float64(report.Bounded) + 5*float64(report.Recoverable)) / n
	}
	return report
}

func optionalEdge(n *SerializableNode, i int) bool {
	for _, edge := range n.Edges {
		if edge.ChildIndex == i && (edge.Type == EdgeRecovery || (edge.Type == EdgeGuard && edge.Condition != "")) {
			return true
		}
	}
	return false
}

func executableChildren(n *SerializableNode) []SerializableNode {
	switch n.Type {
	case "Action", "ChainAction", "Condition", "CachedCondition", "AlwaysSucceed", "SubTreeRef":
		return nil
	case "QualityGate":
		return n.Children[:min(len(n.Children), 2)]
	case "FileTask", "Timeout", "Retry", "CheckpointVerifier", "Budget", "RateLimit", "CircuitBreaker", "Inverter", "Succeeder", "Repeater", "Runner", "Monitor", "AbortOnEvent", "SemaphoreGuard":
		return n.Children[:min(len(n.Children), 1)]
	case "Sequence", "MemSequence", "PersistentMemSequence", "Selector", "MemSelector", "UtilitySelector", "BanditSelector", "DecisionTree", "PlannerNode", "Parallel", "ReactiveParallel", "HumanApprovalGate", "ForEachTask", "ReviewCycle", "ClaudeErrorHandler":
		return n.Children
	}
	return nil
}

// PreservesGovernance rejects candidates that gain score by deleting task work
// or stripping its existing controls. Replacing a capability needs a separate
// task-specific equivalence evaluation; shared history cannot establish it.
func PreservesGovernance(before, after *SerializableNode) bool {
	if before == nil || after == nil {
		return false
	}
	if !preservesFileTasks(before, after) {
		return false
	}
	a, b := AssessGovernance(before), AssessGovernance(after)
	if a.WorkNodes > 0 && b.WorkNodes == 0 {
		return false
	}
	for key, required := range a.controls {
		available := slices.Clone(b.controls[key])
		for _, control := range required {
			found := false
			for i, candidate := range available {
				if candidate.flags&control.flags == control.flags && preservesContracts(control.contracts, candidate.contracts) {
					available = append(available[:i], available[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func subtreeHasTaskWork(n *SerializableNode) bool {
	if IsTaskWork(n) {
		return true
	}
	for i := range executableChildren(n) {
		if subtreeHasTaskWork(&n.Children[i]) {
			return true
		}
	}
	return false
}

// IsTaskWork excludes setup, bookkeeping and recovery from task capability.
// Removing all primary work must not turn a recovery skeleton into an elite.
func IsTaskWork(n *SerializableNode) bool {
	if n.Type != "Action" && n.Type != "ChainAction" {
		return false
	}
	if strings.HasPrefix(n.Name, "Setup") || strings.HasPrefix(n.Name, "ApplyGoapEffects:") {
		return false
	}
	switch n.Name {
	case "SelfCorrect", "EscalateToDeepSeek", "ReflectOnOutcome", "UpdateBehaviorTree", "RecordMetrics", "MarkSuccessful", "AlwaysSucceed", "NoOp":
		return false
	}
	return true
}

func isMandatoryInputGuard(n *SerializableNode) bool {
	if n.Type == "Condition" {
		switch n.Name {
		case "ValidateInput", "TaskIsNotEmpty", "HasClearTask":
			return true
		}
		return strings.HasPrefix(n.Name, "GoapStateMatches:")
	}
	if n.Type == "Sequence" || n.Type == "MemSequence" {
		for i := range n.Children {
			if !optionalEdge(n, i) && isMandatoryInputGuard(&n.Children[i]) {
				return true
			}
		}
	}
	return false
}

func isMandatoryResultCheck(n *SerializableNode) bool {
	if n.Type != "Condition" {
		return false
	}
	switch n.Name {
	case "ValidateOutput":
		return true
	}
	return false
}

func hasPostconditionContract(n *SerializableNode) bool {
	_, err := ParseCheckpointContract(n)
	return err == nil
}

func nonemptyMetadata(n *SerializableNode, key string) bool {
	v, _ := n.Metadata[key].(string)
	return strings.TrimSpace(v) != ""
}

func hasExecutionBudget(n *SerializableNode) bool {
	keys := []string{}
	switch n.Type {
	case "ChainAction":
		keys = []string{"max_tokens"}
	case "Budget":
		if n.MaxRetries > 0 {
			return true
		}
		keys = []string{"max_tokens", "max_ticks"}
	}
	for _, key := range keys {
		switch v := n.Metadata[key].(type) {
		case int:
			if v > 0 {
				return true
			}
		case float64:
			if v > 0 {
				return true
			}
		}
	}
	return false
}

func boundedRecovery(n *SerializableNode) bool {
	return n.Type == "Retry" && n.MaxRetries > 0 && n.MaxRetries <= 5 && len(n.Children) > 0
}

// Descriptions are documentation; only executable chain instructions count.
func hasAgentGuidance(n *SerializableNode) bool {
	if n.Type != "ChainAction" {
		return false
	}
	if nonemptyMetadata(n, "system_msg") {
		return true
	}
	_, prompt, found := strings.Cut(n.Name, ":")
	if found && strings.TrimSpace(prompt) != "" {
		return len(strings.Fields(prompt)) >= 4
	}
	return nonemptyMetadata(n, "prompt")
}

// Automatic evolution must retain the declared task contract on the same work.
// Stronger additional gates are allowed; rewriting the contract requires an
// explicit task-definition change rather than a fitness optimization.
func preservesContracts(required, available []string) bool {
	for _, contract := range required {
		if !slices.Contains(available, contract) {
			return false
		}
	}
	return true
}

// Effectful capability declarations cannot be deleted or redirected by a
// structurally higher-scoring descendant.
func preservesFileTasks(before, after *SerializableNode) bool {
	collect := func(tree *SerializableNode) map[string]int {
		specs := map[string]int{}
		var walk func(*SerializableNode)
		walk = func(n *SerializableNode) {
			if n.Type == "FileTask" {
				spec, err := ParseFileTask(n)
				if err != nil {
					specs["invalid"]++
					return
				}
				contract, _ := ParseResultContract(n)
				data, _ := json.Marshal([]any{spec, contract, n.Metadata["user"], n.Metadata["task"]})
				specs[string(data)]++
			}
			for i := range n.Children {
				walk(&n.Children[i])
			}
		}
		walk(tree)
		return specs
	}
	required, candidate := collect(before), collect(after)
	for key, count := range required {
		if key == "invalid" || candidate[key] < count {
			return false
		}
	}
	return true
}
