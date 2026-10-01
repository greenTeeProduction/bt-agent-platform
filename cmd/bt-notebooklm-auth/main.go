// bt-notebooklm-auth applies the same background-safe policy used by bt-agent.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nico/go-bt-evolve/internal/notebooklmauth"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) == 2 && os.Args[1] == "--mcp" {
		cmd := notebooklmauth.MCPCommand(ctx)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		reliability.BindCommandCancellation(cmd)
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "NotebookLM MCP stopped:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: bt-notebooklm-auth [--mcp]")
		os.Exit(2)
	}
	r := notebooklmauth.Ensure(ctx)
	fmt.Println(r.String())
	if !r.OK() {
		os.Exit(1)
	}
}
