package main

import (
	"context"
	"embed"
	"os"

	"hauler.dev/go/hauler/cmd/hauler/cli"
	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
)

//go:embed binaries/*
var binaries embed.FS

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.NewLogger(os.Stdout)
	ctx = logger.WithContext(ctx)

	// pass the embedded binaries to the cli package
	if err := cli.New(ctx, binaries, &flags.CliRootOpts{}).ExecuteContext(ctx); err != nil {
		logger.Errorf("%v", err)
		cancel()
		os.Exit(1)
	}
}
