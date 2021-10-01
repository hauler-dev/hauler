package app

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rancherfederal/hauler/pkg/log"

	"github.com/spf13/cobra"
)

var (
	level   string
	timeout time.Duration
)

type storePath string

type rootOpts struct {
	logger log.Logger

	datadir string
	cfgdir  string
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
	cmd.AddCommand(NewRegistryCommand())
	cmd.AddCommand(NewDriverCommand())

	f := cmd.PersistentFlags()
	f.StringVarP(&level, "level", "l", "info",
		"Log level (trace, debug, info: default, warn, error, fatal, panic)")
	f.DurationVar(&timeout, "timeout", 1*time.Minute,
		"TODO: timeout for operations")

	return cmd
}

// TODO: Should this be added as a PersistentPreRunE instead?
func initConfig() {
	// Setup user directories used by hauler
	datadir, err := getDataDir()
	cobra.CheckErr(err)
	ro.datadir = datadir

	cfgdir, err := getCfgDir()
	cobra.CheckErr(err)
	ro.cfgdir = cfgdir

	logger := log.NewLogger(os.Stdout, level)
	ro.logger = logger
}

func (o *rootOpts) Logger() log.Logger {
	return o.logger
}

// newStorePath ensures that absolute paths are always used when referring to hauler's storage location
func (o *rootOpts) newStorePath(path string) storePath {
	p, err := filepath.Abs(path)
	if err != nil {
		o.logger.Warnf("Error converting %s to an absolute path, using default hauler storage path instead")
	}

	if err != nil || path == "" {
		o.logger.Debugf("Using users default home directory as store path root")
		p = filepath.Join(o.datadir, "store")
	}

	return storePath(p)
}

func (s storePath) Path() string { return string(s) }

func getDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(home, ".local/hauler")

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}

	return dir, nil
}

func getCfgDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(cfg, "hauler")

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}

	return dir, nil
}
