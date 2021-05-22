package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

type relocateArtifactsOpts struct {
	relocateOpts
}

func NewRelocateArtifactsCommand() *cobra.Command {
	opts := &relocateArtifactsOpts{}

	cmd := &cobra.Command{
		Use:     "artifacts",
		Short:   "relocate artifacts",
		Long:    "",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o *relocateArtifactsOpts) Run() error {
	fmt.Println("relocate artifacts")
	return nil
}
