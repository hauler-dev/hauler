package main

import (
	"context"
	"os"

	"hauler.dev/go/hauler/cmd/hauler/cli"
	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.NewLogger(os.Stdout)
	ctx = logger.WithContext(ctx)

	if err := cli.New(ctx, &flags.CliRootOpts{}).ExecuteContext(ctx); err != nil {
		logger.Errorf("%v", err)
		cancel()
		os.Exit(1)
	}
}
