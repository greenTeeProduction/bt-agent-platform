// bt-notebooklm-auth applies the same background-safe policy used by bt-agent.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nico/go-bt-evolve/internal/notebooklmauth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := notebooklmauth.Ensure(ctx)
	fmt.Println(r.String())
	if !r.OK() {
		os.Exit(1)
	}
}
