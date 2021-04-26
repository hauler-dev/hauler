package app

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"os"
	"time"
)

var (
	cfgFile string
	loglevel string
	timeout time.Duration
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "haulerctl",
		Short: "haulerctl provides CLI-based air-gap migration assistance",
		Long: `haulerctl provides CLI-based air-gap migration assistance using k3s.

Choose your functionality and create a package when internet access is available,
then deploy the package into your air-gapped environment.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := setupLogger(os.Stdout, loglevel); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cobra.OnInitialize(initConfig)

	cmd.AddCommand(NewPackageCommand())
	cmd.AddCommand(NewDeployCommand())
	cmd.AddCommand(NewBundleCommand())

	f := cmd.PersistentFlags()
	f.StringVarP(&loglevel, "loglevel", "l", "info",
		"Log level (debug, info, warn, error, fatal, panic)")
	f.StringVarP(&cfgFile, "config", "c", "./hauler.yaml",
		"config file (./hauler.yaml)")
	f.DurationVar(&timeout, "timeout", 1*time.Minute,
		"timeout for operations")

	return cmd
}

// TODO: Add more
func initConfig() {
	viper.AutomaticEnv() 	// read in any environment variables that match flags
}

func setupLogger(out io.Writer, level string) error {
	log.SetOutput(out)
	lvl, err := log.ParseLevel(level)
	if err != nil {
		return err
	}
	log.SetLevel(lvl)
	return nil
}

// homeDir gets the location of the users home directory. Will be needed for eventual KUBECONFIG searching
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}