package app

import (
	"github.com/spf13/cobra"
)

func NewDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy a k3s cluster serving all dependencies in a generated package.

Given the package archive generated from package, deploy the k3s cluster used to serve all packaged
dependencies.`,
	}

	return cmd
}

type DeployOptions struct {
	// ImageLists    []string
	// ImageArchives []string
}

// Complete takes the command arguments and infers any remaining options.
func (o *DeployOptions) Complete() error {
	return nil
}

// Validate checks the provided set of options.
func (o *DeployOptions) Validate() error {
	return nil
}

// Run performs the operation.
func (o *DeployOptions) Run() error {
	return nil
}
