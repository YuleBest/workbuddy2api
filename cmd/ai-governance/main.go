package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"workbuddy2api/internal/aigovernance"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := aigovernance.Runner{
		Logger: aigovernance.Logger{Out: os.Stdout, Err: os.Stderr},
		Stdin:  os.Stdin,
	}
	if err := runner.Run(ctx); err != nil {
		aigovernance.Logger{Out: os.Stdout, Err: os.Stderr}.Error("%v", err)
		os.Exit(1)
	}
}
