// internal/engine/error_handler_claude.go
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

const (
	// errorHandlerAllowedTools keeps the proposal run read-only: Claude
	// proposes a node as JSON; it never edits the repo (spec §4).
	errorHandlerAllowedTools  = "Read,Glob,Grep"
	errorHandlerClaudeTimeout = 180 * time.Second
	errorHandlerMaxProposal   = 10 // max nodes in a proposal
	errorHandlerMaxDepth      = 4
	errorHandlerErrExcerpt    = 200  // chars of error text in the signature
	errorHandlerSubtreeLimit  = 4000 // chars of subtree JSON in the prompt
)

// errorHandlerClaudeRunner is swappable in tests (same seam pattern as
// defaultSuperpowersClaudeRunner).
var errorHandlerClaudeRunner ClaudeRunner = execClaudeRunner{AllowedTools: errorHandlerAllowedTools}

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
func errorHandlerSignatureFromBB(b *Blackboard, handlerName string) string {
	var cat, node, errText string
	if b.ChainState != nil {
		cat, _ = b.ChainState["last_error_category"].(string)
		node, _ = b.ChainState["last_error_node"].(string)
		errText, _ = b.ChainState["last_error"].(string)
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
// Claude's output (which may wrap it in prose or ```json fences).
func parseErrorHandlerProposal(output string) (errorHandlerProposal, error) {
	rest := output
	for try := 0; try < 5; try++ {
		idx := strings.Index(rest, "{")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		var p errorHandlerProposal
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&p); err == nil {
			if p.Resolvable && p.Node == nil {
				return errorHandlerProposal{}, fmt.Errorf("claude proposal marked resolvable but has no node")
			}
			return p, nil
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
	return walk(node)
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
	subtree, _ := json.MarshalIndent(failing, "", "  ")
	subtreeStr := string(subtree)
	if len(subtreeStr) > errorHandlerSubtreeLimit {
		subtreeStr = subtreeStr[:errorHandlerSubtreeLimit] + "\n… (truncated)"
	}
	allowed := make([]string, 0, len(errorHandlerAllowedNodeTypes))
	for t := range errorHandlerAllowedNodeTypes {
		allowed = append(allowed, t)
	}
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
- The node must be a guard-first composition: its first-ticked leaf MUST be a Condition, typically "LastErrorCategoryIs:%s" or "LastErrorNodeIs:%s", so it never fires on unrelated failures.
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
// the ledger on EVERY outcome so the cooldown always engages.
func requestErrorHandlerProposal(handlerName string, failing *evolution.SerializableNode, b *Blackboard, sig string) (errorHandlerProposal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), errorHandlerClaudeTimeout)
	defer cancel()
	res := errorHandlerClaudeRunner.RunClaude(ctx, goapFusionRepo, buildErrorHandlerPrompt(handlerName, failing, b))
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
