package app

import (
	"github.com/spf13/cobra"
)

func NewDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy all dependencies from a generated package.

Given an archive generated from the package command, deploy all needed
components to serve packaged dependencies.`,
	}

	return cmd
}

type DeployOptions struct {
}

// Preprocess infers any remaining options and performs any required validation.
func (o *DeployOptions) Preprocess() error {
	return nil
}

// Run performs the operation.
func (o *DeployOptions) Run() error {
	return nil
}
