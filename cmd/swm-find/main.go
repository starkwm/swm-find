package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/starkwm/swm-find/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := cli.Runner{Getenv: os.Getenv}
	code := runner.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
