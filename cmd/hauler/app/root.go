package app

import (
	"io"
	"os"
	"time"

	"github.com/rancherfederal/hauler/pkg/log"

	"github.com/spf13/cobra"
)

var (
	loglevel string
	timeout  time.Duration

	getLong = `hauler provides CLI-based air-gap migration assistance using k3s.

Choose your functionality and new a package when internet access is available,
then deploy the package into your air-gapped environment.
`

	getExample = `
hauler pkg build
hauler pkg run pkg.tar.zst

hauler relocate artifacts localhost:5000/artifacts:test artifacts.tar.zst
hauler relocate images localhost:5000 pkg.tar.zst

hauler copy localhost:5000/artifacts:latest
`
)

type rootOpts struct {
	logger log.Logger
}

var ro rootOpts

// NewRootCommand defines the root hauler command
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "hauler",
		Short:        "hauler provides CLI-based air-gap migration assistance",
		Long:         getLong,
		Example:      getExample,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l, err := setupCliLogger(os.Stdout, loglevel)
			if err != nil {
				return err
			}

			ro.logger = l
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cobra.OnInitialize()

	cmd.AddCommand(NewRelocateCommand())
	cmd.AddCommand(NewCopyCommand())
	cmd.AddCommand(NewPkgCommand())

	f := cmd.PersistentFlags()
	f.StringVarP(&loglevel, "loglevel", "l", "info",
		"Log level (debug, info, warn, error, fatal, panic)")
	f.DurationVar(&timeout, "timeout", 1*time.Minute,
		"TODO: timeout for operations")

	return cmd
}

func setupCliLogger(out io.Writer, level string) (log.Logger, error) {
	l := log.NewLogger(out, level)

	return l, nil
}
