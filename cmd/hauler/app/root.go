package app

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rancherfederal/hauler/pkg/log"

	"github.com/spf13/cobra"
)

const (
	HaulerDefaultPath = ".local/hauler"
)

var (
	level   string
	timeout time.Duration
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
		Long:         ``,
		Example:      ``,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cobra.OnInitialize(initConfig)

	cmd.AddCommand(NewPackageCommand())
	// cmd.AddCommand(NewPkgCommand())
	cmd.AddCommand(NewRegistryCommand())

	f := cmd.PersistentFlags()
	f.StringVarP(&level, "level", "l", "debug",
		"Log level (trace, debug, info: default, warn, error, fatal, panic)")
	f.DurationVar(&timeout, "timeout", 1*time.Minute,
		"TODO: timeout for operations")

	return cmd
}

func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	err = os.MkdirAll(filepath.Join(home, HaulerDefaultPath), os.ModePerm)
	cobra.CheckErr(err)

	cfgDir, err := os.UserConfigDir()
	_ = cfgDir

	logger := log.NewLogger(os.Stdout, level)
	ro.logger = logger
}

func (o *rootOpts) Logger() log.Logger {
	return o.logger
}
