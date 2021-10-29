package main

import (
	"context"
	"os"

	"github.com/rancherfederal/hauler/cmd/hauler/cli"
	"github.com/rancherfederal/hauler/pkg/log"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.NewLogger(os.Stdout)
	ctx = logger.WithContext(ctx)

	if err := cli.New().ExecuteContext(ctx); err != nil {
		logger.Errorf("%v", err)
	}
}
