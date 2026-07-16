// internal/engine/error_handler_claude.go
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

const (
	// errorHandlerAllowedTools keeps the proposal run read-only: Claude
	// proposes a node as JSON; it never edits the repo (spec §4).
	errorHandlerAllowedTools   = "Read,Glob,Grep"
	errorHandlerClaudeTimeout  = 180 * time.Second
	errorHandlerMaxProposal    = 10 // max nodes in a proposal
	errorHandlerMaxDepth       = 4
	errorHandlerErrExcerpt     = 200  // chars of error text in the signature
	errorHandlerSubtreeLimit   = 4000 // chars of subtree JSON in the prompt
	errorHandlerPromptErrLimit = 500  // chars of untrusted error text in the prompt
)

// errorHandlerClaudeRunner is swappable in tests (same seam pattern as
// defaultSuperpowersClaudeRunner). ForceReadOnly: the proposal run's security
// contract is its Read,Glob,Grep tool list — the skip-permissions env override
// must never widen it.
var errorHandlerClaudeRunner ClaudeRunner = execClaudeRunner{AllowedTools: errorHandlerAllowedTools, ForceReadOnly: true}

func errorHandlerEnabled() bool {
	return !strings.EqualFold(os.Getenv("BT_CLAUDE_ERROR_HANDLER"), "off")
}

func errorHandlerCooldown() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("BT_ERROR_HANDLER_COOLDOWN")); err == nil && d > 0 {
		return d
	}
	return 6 * time.Hour
}

func errorHandlerMaxNodes() int {
	if n, err := strconv.Atoi(os.Getenv("BT_ERROR_HANDLER_MAX_NODES")); err == nil && n > 0 {
		return n
	}
	return 5
}

func stripDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return -1
		}
		return r
	}, s)
}

// errorHandlerSignatureFromBB identifies an error class: same tree + failing
// node + category + digit-stripped error text prefix ⇒ same signature, so
// timestamps/counters in messages don't defeat the cooldown ledger.
//
// When the tree has NO reliability wiring (last_error_category AND
// last_error_node both empty), the only available text is free-form bb.Result,
// which is near-unique per run (hex ids, UUIDs, paths survive digit-stripping)
// — hashing it would mint a fresh signature per failing run, defeating the
// cooldown entirely and growing the ledger unbounded. Collapse that case to a
// COARSE stable key: handler + protected subtree root only.
func errorHandlerSignatureFromBB(b *Blackboard, handlerName, protectedName string) string {
	var cat, node, errText string
	if b.ChainState != nil {
		cat, _ = b.ChainState["last_error_category"].(string)
		node, _ = b.ChainState["last_error_node"].(string)
		errText, _ = b.ChainState["last_error"].(string)
	}
	if cat == "" && node == "" {
		sum := sha256.Sum256([]byte(handlerName + "|" + protectedName))
		return hex.EncodeToString(sum[:])[:12]
	}
	if errText == "" {
		errText = b.Result
	}
	if len(errText) > errorHandlerErrExcerpt {
		errText = errText[:errorHandlerErrExcerpt]
	}
	sum := sha256.Sum256([]byte(handlerName + "|" + node + "|" + cat + "|" + stripDigits(errText)))
	return hex.EncodeToString(sum[:])[:12]
}

type errorHandlerProposal struct {
	Resolvable bool                        `json:"resolvable"`
	Reason     string                      `json:"reason"`
	Node       *evolution.SerializableNode `json:"node"`
}

// parseErrorHandlerProposal extracts the first parseable JSON object from
// Claude's output (which may wrap it in prose or ```json fences). Only objects
// that actually carry the "resolvable" contract key count — a bare "{}" or an
// echoed example node object must not decode as {resolvable:false}.
func parseErrorHandlerProposal(output string) (errorHandlerProposal, error) {
	rest := output
	for {
		idx := strings.Index(rest, "{")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&raw); err == nil {
			if _, ok := raw["resolvable"]; ok {
				var p errorHandlerProposal
				if err := json.NewDecoder(strings.NewReader(rest)).Decode(&p); err == nil {
					if p.Resolvable && p.Node == nil {
						return errorHandlerProposal{}, fmt.Errorf("claude proposal marked resolvable but has no node")
					}
					return p, nil
				}
			}
		}
		rest = rest[1:]
	}
	return errorHandlerProposal{}, fmt.Errorf("no parseable JSON proposal in claude output (%d bytes)", len(output))
}

// errorHandlerAllowedNodeTypes is the strict proposal vocabulary (spec §5).
// Deliberately a subset of evolution.KnownNodeTypes: no gates, no subtrees,
// no planners — a recovery node composes existing leaves under basic control
// flow. (MemSequence/MemSelector are absent from KnownNodeTypes, so they
// could never validate anyway.)
var errorHandlerAllowedNodeTypes = map[string]bool{
	"Sequence": true, "Selector": true,
	"Retry": true, "Timeout": true, "Inverter": true, "Succeeder": true,
	"Action": true, "Condition": true, "AlwaysSucceed": true,
}

// errorHandlerDeniedActionSubstrings is a conservative FLOOR — a denylist,
// not an allowlist — of repo/fleet/state-mutating verbs blocked from
// Claude-generated recovery nodes: proposals are auto-applied with no human
// approval, so anything that commits, pushes, deploys, mutates trees/agents/
// worktrees/programs/schedules, or deletes state is out of vocabulary
// regardless of registration (e.g. ApplySuperpowersRunToMainRepo,
// PushBranchAndCreatePR, SuperpowersTaskCommit, RunDeploy, ApplyFusion,
// ApplyTargetedMutation, HermesUpdateAgent, UpdateBehaviorTree,
// ExecuteSuperpowersTaskBatch, RunSuperpowersClaudeImplementation,
// SeedNextProgram, RestartDeadAgents, DiscardSuperpowersWorktree,
// PrepareSuperpowersWorktree, FixBuildErrors, SaveDocument,
// RunScheduledGoapFusionCycle). Case-sensitive substring match on the action
// name. Being absent from this list does NOT make an action safe — it is
// only ever widened, never relied on as exhaustive.
var errorHandlerDeniedActionSubstrings = []string{
	"Apply", "Push", "Deploy", "Commit", "Merge", "Mutation", "Mutate",
	"HermesUpdate", "UpdateBehaviorTree", "SeedProgram", "CreatePR",
	"Delete", "Remove", "Write",
	"Superpowers", "Seed", "Restart", "Worktree", "Fix", "Save", "Fusion",
	"Execute", "Schedule",
}

// firstTickedLeaf follows first children down to the leaf a tick reaches
// first — the proposal's guard position.
func firstTickedLeaf(n *evolution.SerializableNode) *evolution.SerializableNode {
	if n == nil {
		return nil
	}
	switch n.Type {
	case "Action", "Condition", "AlwaysSucceed":
		return n
	}
	if len(n.Children) == 0 {
		return nil
	}
	return firstTickedLeaf(&n.Children[0])
}

func errorHandlerNodeDepth(n *evolution.SerializableNode) int {
	if n == nil {
		return 0
	}
	deepest := 0
	for i := range n.Children {
		if d := errorHandlerNodeDepth(&n.Children[i]); d > deepest {
			deepest = d
		}
	}
	return 1 + deepest
}

// validateErrorHandlerProposal is the strict gate before any graft: the
// engine's permissive unknown-name fallback (tree.go actionForName) is
// explicitly NOT acceptable for generated nodes — every leaf must resolve.
func validateErrorHandlerProposal(node *evolution.SerializableNode, takenNames map[string]bool) error {
	if node == nil {
		return fmt.Errorf("proposal node is nil")
	}
	if strings.TrimSpace(node.Name) == "" {
		return fmt.Errorf("proposal node must have a name")
	}
	if takenNames[node.Name] {
		return fmt.Errorf("proposal name %q already exists on this handler", node.Name)
	}
	if errs := node.Validate(); len(errs) > 0 {
		return fmt.Errorf("proposal failed tree validation: %v", errs)
	}
	if count := evolution.CountNodes(node); count > errorHandlerMaxProposal {
		return fmt.Errorf("proposal has %d nodes, max %d", count, errorHandlerMaxProposal)
	}
	if depth := errorHandlerNodeDepth(node); depth > errorHandlerMaxDepth {
		return fmt.Errorf("proposal depth %d exceeds max %d", depth, errorHandlerMaxDepth)
	}
	guard := firstTickedLeaf(node)
	if guard == nil || guard.Type != "Condition" {
		return fmt.Errorf("proposal's first-ticked leaf must be a Condition guard")
	}
	// Semantic guard enforcement: the guard must be a parameterized error
	// condition with a real value — a registered (possibly broadly-true)
	// condition or an empty-param guard lets an always-true guard plus a
	// vacuous action mask ALL future failures of the tree as recovered success.
	if !isParameterizedErrorGuard(guard.Name) {
		return fmt.Errorf("proposal guard %q must be %s<category> or %s<node> with a non-empty value", guard.Name, errorCategoryCondPrefix, errorNodeCondPrefix)
	}
	// The guard must be reachable through Sequence nodes only: a Succeeder/
	// Inverter/Selector above it can neutralize the guard so the composition
	// succeeds even when the guard fails.
	for n := node; n != guard; n = &n.Children[0] {
		if n.Type != "Sequence" {
			return fmt.Errorf("every node above the guard must be a Sequence, found %q (%s)", n.Type, n.Name)
		}
	}
	actionLeafCount := 0
	var walk func(n *evolution.SerializableNode) error
	walk = func(n *evolution.SerializableNode) error {
		if !errorHandlerAllowedNodeTypes[n.Type] {
			return fmt.Errorf("node type %q not allowed in proposals", n.Type)
		}
		switch n.Type {
		case "Action":
			if GetAction(n.Name) == nil {
				return fmt.Errorf("action %q is not registered", n.Name)
			}
			for _, denied := range errorHandlerDeniedActionSubstrings {
				if strings.Contains(n.Name, denied) {
					return fmt.Errorf("action %q is not allowed in generated recovery nodes (mutating action, matches denied token %q)", n.Name, denied)
				}
			}
			actionLeafCount++
		case "Condition":
			if GetCondition(n.Name) == nil && errorHandlerConditionFor(n.Name) == nil {
				return fmt.Errorf("condition %q is not registered", n.Name)
			}
		}
		for i := range n.Children {
			if err := walk(&n.Children[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(node); err != nil {
		return err
	}
	// A proposal that is only a guard (no Action leaf) always succeeds once
	// the guard matches — it would mark EVERY recurrence of that error
	// category as recovered Success forever, defeating the honest failure
	// signal the tree relies on.
	if actionLeafCount == 0 {
		return fmt.Errorf("proposal must contain at least one Action node (a guard-only recovery masks failures)")
	}
	return nil
}

func buildErrorHandlerPrompt(handlerName string, failing *evolution.SerializableNode, b *Blackboard) string {
	var cat, errNode, errText string
	if b.ChainState != nil {
		cat, _ = b.ChainState["last_error_category"].(string)
		errNode, _ = b.ChainState["last_error_node"].(string)
		errText, _ = b.ChainState["last_error"].(string)
	}
	if errText == "" {
		errText = b.Result
	}
	// Truncate the error text: it is untrusted failure output (a prompt-
	// injection channel) — cap the excerpt like the signature does.
	if len(errText) > errorHandlerPromptErrLimit {
		errText = errText[:errorHandlerPromptErrLimit] + "… (truncated)"
	}
	subtree, _ := json.MarshalIndent(failing, "", "  ")
	subtreeStr := string(subtree)
	if len(subtreeStr) > errorHandlerSubtreeLimit {
		subtreeStr = subtreeStr[:errorHandlerSubtreeLimit] + "\n… (truncated)"
	}
	allowed := make([]string, 0, len(errorHandlerAllowedNodeTypes))
	for t := range errorHandlerAllowedNodeTypes {
		allowed = append(allowed, t)
	}
	sort.Strings(allowed) // deterministic prompt (map iteration order is random)
	return fmt.Sprintf(`You are the error handler for a Go behavior-tree agent platform. A subtree failed and you may propose ONE recovery node to handle this class of error in future runs.

## Failure context
- Handler: %s
- Failing node: %s
- Error category: %s
- Failure count this run: %d
- Error text:
%s

## Failing subtree (JSON)
%s

## Rules for your proposal
- Compose ONLY registered action/condition names listed below — you cannot invent new behavior.
- The node must be a guard-first composition: its first-ticked leaf MUST be the Condition "LastErrorCategoryIs:<category>" or "LastErrorNodeIs:<node-name>" with a real, non-empty value (e.g. "LastErrorCategoryIs:%s" or "LastErrorNodeIs:%s"), so it never fires on unrelated failures. Every node on the path from the root down to that guard must be a Sequence — no Succeeder/Inverter/Selector above the guard.
- Allowed node types: %s
- Max 10 nodes, max depth 4. Give the root and every composite a short unique descriptive name.
- Node JSON shape: {"type": "...", "name": "...", "children": [...], "max_retries": N (Retry only), "timeout_ms": N (Timeout only)}

## Registered actions
%s

## Registered conditions
%s
(Parameterized conditions also available: "LastErrorCategoryIs:<category>", "LastErrorNodeIs:<node-name>")

## Reply contract
Reply with ONLY one JSON object, no prose:
{"resolvable": true, "reason": "<why this handles the error>", "node": {…}}
or, if this error cannot be handled by composing the registered vocabulary:
{"resolvable": false, "reason": "<what capability is missing>"}`,
		handlerName, errNode, cat, b.FailureCount, errText, subtreeStr, cat, errNode,
		strings.Join(allowed, ", "),
		strings.Join(RegisteredActionNames(), ", "),
		strings.Join(RegisteredConditionNames(), ", "))
}

// requestErrorHandlerProposal makes the single guarded Claude call and stamps
// the ledger on EVERY outcome so the cooldown always engages. ctx is the
// tick's run context (RunTask's tree deadline) so the call — which holds the
// fleet-wide claude.lock — cannot outlive the tree budget; the 180s cap
// applies on top of it.
func requestErrorHandlerProposal(ctx context.Context, handlerName string, failing *evolution.SerializableNode, b *Blackboard, sig string) (errorHandlerProposal, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, errorHandlerClaudeTimeout)
	defer cancel()
	res := errorHandlerClaudeRunner.RunClaude(callCtx, goapFusionRepo, buildErrorHandlerPrompt(handlerName, failing, b))
	if res.Err != nil {
		errorHandlerLedgerStamp(sig, "error")
		return errorHandlerProposal{}, fmt.Errorf("claude error-handler call failed: %w", res.Err)
	}
	p, err := parseErrorHandlerProposal(res.Output)
	if err != nil {
		errorHandlerLedgerStamp(sig, "error")
		return errorHandlerProposal{}, err
	}
	if !p.Resolvable {
		errorHandlerLedgerStamp(sig, "unresolvable")
		Warn("claude error handler: error judged unresolvable with registered vocabulary",
			"handler", handlerName, "signature", sig, "reason", p.Reason)
		return p, nil
	}
	errorHandlerLedgerStamp(sig, "proposed")
	return p, nil
}
