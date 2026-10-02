// Command miband fetches Xiaomi Smart Band data that Mi Fitness synced to the Xiaomi cloud.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := newRootCmd().ExecuteContext(ctx)

	ctxErr := ctx.Err()
	stop()

	if ctxErr != nil {
		os.Exit(exitINT)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCode(err))
	}
}
