package blocks

import (
	"context"
	"time"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// annotateBlockSource tags every node in an expanded block tree for metrics/traces.
func annotateBlockSource(node *evolution.SerializableNode, blockID string) {
	if node == nil || blockID == "" {
		return
	}
	if node.Metadata == nil {
		node.Metadata = make(map[string]any)
	}
	node.Metadata["block_id"] = blockID
	for i := range node.Children {
		annotateBlockSource(&node.Children[i], blockID)
	}
}

func traceBlockOp(ctx context.Context, operation, blockID string, fn func(context.Context) error) error {
	spanName := "blocks." + operation
	if blockID != "" {
		spanName += "/" + blockID
	}
	spanCtx, span := tracing.StartSpan(ctx, spanName)
	defer span.End()
	span.SetAttribute("block.operation", operation)
	if blockID != "" {
		span.SetAttribute("block.id", blockID)
	}
	start := time.Now()
	err := fn(spanCtx)
	status := "ok"
	if err != nil {
		status = "error"
		span.RecordError(err)
	}
	dashboard.RecordBlockOp(operation, blockID, status, time.Since(start).Milliseconds())
	return err
}
