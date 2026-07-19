// internal/engine/error_handler_claude.go
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Deliberately do NOT fall back to b.Result here. A category set WITHOUT an
	// explicit last_error is the ClaudeErrorHandler classifying an otherwise-
	// unclassified failure (error_handler_classify.go): b.Result is still the
	// near-unique free text, so hashing it would defeat the cooldown just as in
	// the coarse case above. With errText empty the signature keys on
	// handler|node|category alone — stable across recurrences. The prompt keeps
	// its own b.Result fallback (buildErrorHandlerPrompt) for readable context.
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
	// CodeFix is an OPTIONAL escalation carried only on an unresolvable verdict
	// (self-fixing fleet Part A, spec §2): when a tree failure cannot be
	// recovered at runtime by composing actions BUT is a genuine source-code bug,
	// Claude describes the fix here so the node can seed a code-fix program. Nil
	// for the ordinary two branches (resolvable node / plain unresolvable).
	CodeFix *errorHandlerCodeFix `json:"code_fix"`
}

// errorHandlerCodeFix is the escalation payload: a file-scoped, TDD-able
// description of a source bug the runtime-recovery vocabulary can't fix. The
// milestone is the instruction the goap loop RED→GREENs; validateCodeFix gates
// it before it seeds anything.
type errorHandlerCodeFix struct {
	IsBug     bool     `json:"is_bug"`
	Title     string   `json:"title"`
	Milestone string   `json:"milestone"`
	Files     []string `json:"files"`
	Rationale string   `json:"rationale"`
}

// validateCodeFix gates an escalation before it seeds a code-fix program. It is
// deliberately strict on the fields the goap loop needs to RED→GREEN a fix — a
// real is_bug flag, a title, a file-scoped milestone, and at least one plausible
// repo file path (contains "/" or ends ".go") — and soft-checks that the
// milestone actually names one of those files, since a milestone that references
// no file at all makes the goap loop flail. Runtime-recoverable / transient
// failures carry no code_fix and never reach here.
func validateCodeFix(cf *errorHandlerCodeFix) error {
	if cf == nil {
		return fmt.Errorf("code_fix is nil")
	}
	if !cf.IsBug {
		return fmt.Errorf("code_fix.is_bug must be true to escalate")
	}
	if strings.TrimSpace(cf.Title) == "" {
		return fmt.Errorf("code_fix.title must be non-empty")
	}
	if strings.TrimSpace(cf.Milestone) == "" {
		return fmt.Errorf("code_fix.milestone must be non-empty")
	}
	var files []string
	for _, f := range cf.Files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") && !strings.HasSuffix(f, ".go") {
			continue // not a plausible repo path
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return fmt.Errorf("code_fix.files must contain at least one plausible repo path")
	}
	// Soft check: the milestone must name at least one of the files (full path or
	// basename) so the loop has a concrete file to fix. Reject only if it names none.
	named := false
	for _, f := range files {
		if strings.Contains(cf.Milestone, f) || strings.Contains(cf.Milestone, filepath.Base(f)) {
			named = true
			break
		}
	}
	if !named {
		return fmt.Errorf("code_fix.milestone must reference at least one of the files")
	}
	// I2(b) — the sharpest defense: deny an escalation that targets a self-fix
	// guard file itself (self_fix_seed.go, error_handler_claude/node/store.go,
	// actions_self_review.go). Without this, the error-handler could escalate
	// (and the goap loop then auto-apply) a "fix" that quietly weakens its own
	// guards — e.g. raising selfFixMaxOpen's cap. Guard changes require a human.
	// Scans Files AND the free-text Milestone (not Files alone): the Milestone
	// is the instruction the downstream TDD implementer actually executes with
	// unrestricted Read/Write/Edit, so an innocuous Files list paired with a
	// Milestone that names a guard file must be caught too.
	if mentionsSelfFixGuardFile(append(append([]string{}, cf.Files...), cf.Milestone)...) {
		return fmt.Errorf("code_fix targets a self-fix guard file; guard changes require a human")
	}
	return nil
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
// flow. This is an explicit, independent allowlist — it does not rely on
// evolution.KnownNodeTypes to exclude anything on its behalf. (MemSelector is
// still absent from KnownNodeTypes and so could never validate there either;
// MemSequence was added to KnownNodeTypes for runtime tree mutation's
// cursor-migration support, but stays excluded here explicitly.)
var errorHandlerAllowedNodeTypes = map[string]bool{
	"Sequence": true, "Selector": true,
	"Retry": true, "Timeout": true, "Inverter": true, "Succeeder": true,
	"Action": true, "Condition": true, "AlwaysSucceed": true,
}

// errorHandlerActionAllowlist is the exact set of registered actions a
// Claude-proposed recovery node may compose. This is the security boundary for
// the auto-executing recovery path: default-deny, exact-name (not substring),
// and every entry verified recovery-safe — blackboard-only or a single bounded
// LLM call, none mutating the repo, fleet, filesystem, or external services.
// A registered action NOT in this set is rejected even though it exists.
var errorHandlerActionAllowlist = map[string]bool{
	"DefaultFallback": true, "MarkSuccessful": true, "SelfCorrect": true,
	"EscalateToDeepSeek": true, "ClearNodeError": true,
	"HandleTimeoutError": true, "HandleTransientError": true,
	"HandleValidationError": true, "HandleCircuitOpen": true,
	"RollbackOnFailure": true, "EscalateToOperator": true,
	"SendAlert": true, "UpdateBlackboard": true,
}

// errorHandlerExtraAllowedActions is nil in production; tests populate it via
// allowErrorHandlerTestActions so their eh_* test actions can validate without
// polluting the production allowlist above.
var errorHandlerExtraAllowedActions map[string]bool

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
			if !errorHandlerActionAllowlist[n.Name] && !errorHandlerExtraAllowedActions[n.Name] {
				return fmt.Errorf("action %q is not in the recovery-safe allowlist", n.Name)
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
	allowedActions := make([]string, 0, len(errorHandlerActionAllowlist))
	for a := range errorHandlerActionAllowlist {
		allowedActions = append(allowedActions, a)
	}
	sort.Strings(allowedActions) // deterministic prompt (map iteration order is random)
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
- Compose ONLY the allowed recovery actions and registered condition names listed below — you cannot invent new behavior.
- The node must be a guard-first composition: its first-ticked leaf MUST be the Condition "LastErrorCategoryIs:<category>" or "LastErrorNodeIs:<node-name>" with a real, non-empty value (e.g. "LastErrorCategoryIs:%s" or "LastErrorNodeIs:%s"), so it never fires on unrelated failures. Every node on the path from the root down to that guard must be a Sequence — no Succeeder/Inverter/Selector above the guard.
- Allowed node types: %s
- Max 10 nodes, max depth 4. Give the root and every composite a short unique descriptive name.
- Node JSON shape: {"type": "...", "name": "...", "children": [...], "max_retries": N (Retry only), "timeout_ms": N (Timeout only)}

## Allowed recovery actions (compose ONLY these)
%s

## Registered conditions
%s
(Parameterized conditions also available: "LastErrorCategoryIs:<category>", "LastErrorNodeIs:<node-name>")

## Reply contract
Reply with ONLY one JSON object, no prose:
{"resolvable": true, "reason": "<why this handles the error>", "node": {…}}
or, if this error cannot be handled by composing the allowed vocabulary:
{"resolvable": false, "reason": "<what capability is missing>"}
If — and ONLY if — the failure cannot be recovered at runtime by composing the actions above BUT is a genuine SOURCE-CODE bug that a small code fix would resolve (NOT a transient, rate-limit, config, or environment failure), add a "code_fix" object naming the specific file(s) to fix:
{"resolvable": false, "reason": "<...>", "code_fix": {"is_bug": true, "title": "<short title>", "milestone": "<file-scoped TDD instruction: name the file(s), the defect, and the exact fix so an implementer can write a failing test then make it pass>", "files": ["path/to/file.go"], "rationale": "<why this is a real code bug>"}}
Omit "code_fix" entirely for transient/rate-limit/config/environment failures — set it only for a real source-code bug.`,
		handlerName, errNode, cat, b.FailureCount, errText, subtreeStr, cat, errNode,
		strings.Join(allowed, ", "),
		strings.Join(allowedActions, ", "),
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
		// Valid code_fix: do NOT stamp here. The node seeds then stamps
		// escalated | escalate_deferred | escalate_failed so a failed seed
		// cannot cool out under a false "escalated" verdict. Invalid/absent
		// code_fix stays "unresolvable" (today's behavior).
		if p.CodeFix != nil && validateCodeFix(p.CodeFix) == nil {
			Warn("claude error handler: error judged unresolvable; code_fix pending seed",
				"handler", handlerName, "signature", sig, "reason", p.Reason)
			return p, nil
		}
		errorHandlerLedgerStamp(sig, "unresolvable")
		Warn("claude error handler: error judged unresolvable with registered vocabulary",
			"handler", handlerName, "signature", sig, "reason", p.Reason, "verdict", "unresolvable")
		return p, nil
	}
	errorHandlerLedgerStamp(sig, "proposed")
	return p, nil
}
