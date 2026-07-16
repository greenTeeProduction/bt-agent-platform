// internal/engine/error_handler_node.go
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildClaudeErrorHandler builds the self-extending recovery decorator
// (spec: docs/superpowers/specs/2026-07-15-claude-error-handler-design.md).
//
// Child 0 is the protected subtree. Persisted extensions (Claude-proposed
// recovery nodes) are re-grafted here on every build because scheduled runs
// rebuild trees from the compiled catalog each run. On child failure the
// handler ticks matching extensions guard-first; if none handle it, it makes
// ONE guarded read-only Claude call proposing a new recovery node, validates
// it strictly, persists it, ticks it immediately, and otherwise passes the
// failure through unchanged.
func BuildClaudeErrorHandler(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "ClaudeErrorHandler requires a protected child"
			return -1
		})
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	// Sandbox (gardener structural scoring) must never call Claude or touch
	// the store — behave as a transparent passthrough.
	if bb.Sandbox {
		return child
	}
	handlerName := node.Name
	protected := node.Children[0]

	type recovery struct {
		name  string
		guard string // first-ticked Condition name; evaluated before the tick
		cmd   btcore.Command[Blackboard]
	}
	buildRecovery := func(ext ErrorHandlerExtension) recovery {
		extNode := ext.Node
		guard := ""
		if leaf := firstTickedLeaf(&extNode); leaf != nil && leaf.Type == "Condition" {
			guard = leaf.Name
		}
		return recovery{name: extNode.Name, guard: guard, cmd: buildNode(&extNode, bb, handlerName)}
	}
	var recoveries []recovery
	takenNames := map[string]bool{protected.Name: true}
	for _, ext := range activeErrorHandlerExtensions(handlerName) {
		// Re-validate every persisted extension against the CURRENT policy on
		// each build. The action allowlist is the security boundary for this
		// auto-executing path, and it must apply to already-granted extensions
		// and to any hand-edited store — not only to freshly-proposed nodes.
		// Validation happens once at proposal time; without this a tightened
		// allowlist (or a tampered extensions.json) would still graft and tick a
		// now-disallowed node. Skip (do not graft) anything that no longer passes.
		extNode := ext.Node
		if err := validateErrorHandlerProposal(&extNode, map[string]bool{}); err != nil {
			Warn("claude error handler: skipping persisted extension that fails current policy",
				"handler", handlerName, "node", ext.Node.Name, "err", err)
			continue
		}
		recoveries = append(recoveries, buildRecovery(ext))
		takenNames[ext.Node.Name] = true
	}

	markRecovered := func(b *Blackboard, nodeName, sig string) {
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["error_handler_recovered"] = sig
		// The pre-recovery Result routinely contains quality-reject markers
		// ("error:", "failed to", …) from the original failure. RunTask's
		// validateOutputQuality backstop would flip the recovered Success back
		// to Failure on those markers — fence the failure text so
		// stripFencedBlocks removes it from the quality scan, and append the
		// recovery note as clean prose.
		note := fmt.Sprintf("## Error Handler Recovery\nHandler %s recovered via generated node %s (error signature %s).\n", handlerName, nodeName, sig)
		if prior := strings.TrimSpace(b.Result); prior != "" {
			// Neutralize any triple-backtick fences already inside prior: left
			// intact, they would toggle stripFencedBlocks' in-fence state and
			// leak part of the failure text back into RunTask's quality scan,
			// able to re-trip the Success→Failure flip. Tildes still render as
			// a fence in markdown but cannot break the outer ``` wrapper.
			prior = strings.ReplaceAll(prior, "```", "~~~")
			b.Result = fmt.Sprintf("```\n%s\n```\n\n%s", prior, note)
		} else {
			b.Result = note
		}
		Info("claude error handler: recovered", "handler", handlerName, "node", nodeName, "signature", sig)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		code := child.Run(ctx)
		if code >= 0 {
			return code
		}
		b := ctx.Blackboard
		sig := errorHandlerSignatureFromBB(b, handlerName, protected.Name)
		// 1. Existing recovery extensions, guard-first. The guard is evaluated
		// separately from the tick so a guard mismatch (expected on unrelated
		// errors) never counts as a recovery failure toward auto-disable.
		for _, r := range recoveries {
			if r.guard != "" && !b.conditionForName(r.guard)(b) {
				continue
			}
			if r.cmd.Run(ctx) == 1 {
				recordErrorHandlerResult(handlerName, r.name, true)
				markRecovered(b, r.name, sig)
				return 1
			}
			recordErrorHandlerResult(handlerName, r.name, false)
		}
		// 2. Maybe grow a new handler — every guard must pass first.
		if !errorHandlerEnabled() {
			return -1
		}
		if len(activeErrorHandlerExtensions(handlerName)) >= errorHandlerMaxNodes() {
			return -1
		}
		if entry, ok := errorHandlerLedgerGet(sig); ok && time.Since(entry.LastAttempt) < errorHandlerCooldown() {
			return -1
		}
		release, ok := acquireErrorHandlerClaudeLock()
		if !ok {
			return -1 // another agent is already consulting Claude — skip this run
		}
		defer release()
		// Thread the tick's run context (RunTask's tree deadline) into the
		// Claude call so it cannot outlive the tree budget while holding the
		// fleet lock. BTContext embeds context.Context; tests may leave it nil.
		runCtx := context.Background()
		if ctx.Context != nil {
			runCtx = ctx.Context
		}
		prop, err := requestErrorHandlerProposal(runCtx, handlerName, &protected, b, sig)
		if err != nil || !prop.Resolvable {
			return -1
		}
		if err := validateErrorHandlerProposal(prop.Node, takenNames); err != nil {
			errorHandlerLedgerStamp(sig, "rejected")
			Warn("claude error handler: proposal rejected", "handler", handlerName, "signature", sig, "err", err)
			return -1
		}
		ext := ErrorHandlerExtension{Node: *prop.Node, Signature: sig, CreatedAt: time.Now()}
		if err := appendErrorHandlerExtension(handlerName, ext); err != nil {
			Warn("claude error handler: persist failed", "handler", handlerName, "err", err)
			return -1
		}
		Info("claude error handler: tree extended with generated recovery node",
			"handler", handlerName, "node", prop.Node.Name, "signature", sig, "reason", prop.Reason)
		r := buildRecovery(ext)
		recoveries = append(recoveries, r)
		takenNames[r.name] = true
		if r.cmd.Run(ctx) == 1 {
			recordErrorHandlerResult(handlerName, r.name, true)
			markRecovered(b, r.name, sig)
			return 1
		}
		recordErrorHandlerResult(handlerName, r.name, false)
		return -1
	})
}
