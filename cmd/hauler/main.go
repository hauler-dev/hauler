package main

import (
	"context"
	"embed"
	"os"

	"hauler.dev/go/hauler/cmd/hauler/cli"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
)

//go:embed binaries/*
var binaries embed.FS

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.NewLogger(os.Stdout)
	ctx = logger.WithContext(ctx)

	// ensure cosign binary is available
	if err := cosign.EnsureBinaryExists(ctx, binaries); err != nil {
		logger.Errorf("%v", err)
		os.Exit(1)
	}

	if err := cli.New().ExecuteContext(ctx); err != nil {
		logger.Errorf("%v", err)
		cancel()
		os.Exit(1)
	}
}
