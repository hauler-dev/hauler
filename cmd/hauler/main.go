package main

import (
	"context"
	"os"
	"embed"

	"github.com/rancherfederal/hauler/cmd/hauler/cli"
	"github.com/rancherfederal/hauler/pkg/cosign"
	"github.com/rancherfederal/hauler/pkg/log"
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
	}
	
	if err := cli.New().ExecuteContext(ctx); err != nil {
		logger.Errorf("%v", err)
	}
}
