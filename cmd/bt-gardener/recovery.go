package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/gardener"
)

// This offline command exits before daemon/model/scheduler initialization.
func runTreeRecovery(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("recover-persisted-trees", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	dir := flags.String("dir", "", "shared tree directory to inspect (required)")
	apply := flags.Bool("apply", false, "back up and restore the detected collapsed builtins")
	offline := flags.Bool("offline", false, "assert all writers to this directory are stopped")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || flags.NArg() != 0 {
		return fmt.Errorf("explicit --dir required; no positional arguments")
	}
	if *apply && !*offline {
		return fmt.Errorf("--apply requires --offline: legacy writers do not share the recovery lock")
	}
	identity := dashboard.ReadBuildIdentity()
	revision := fmt.Sprintf("%s (dirty=%v)", identity.Revision, identity.Dirty)
	report, err := gardener.PlanTreeRecovery(*dir, revision)
	if err != nil {
		return err
	}
	if *apply {
		err = gardener.ApplyTreeRecovery(ctx, report)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if writeErr := encoder.Encode(report); writeErr != nil {
		return fmt.Errorf("recovery result acknowledgement: %w (operation error: %v)", writeErr, err)
	}
	return err
}
