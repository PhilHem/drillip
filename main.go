package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/PhilHem/drillip/internal/bootstrap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	input := newProcessInput(ctx, os.Stdin)
	defer input.Close()
	return bootstrap.Run(ctx, os.Args[1:], input, os.Stdout, os.Stderr)
}
