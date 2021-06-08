package app

import (
	"fmt"
	"io"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

var (
	cfgFile  string
	loglevel string
	timeout  time.Duration

	getLong = `haulerctl provides CLI-based air-gap migration assistance using k3s.

	Choose your functionality and new a package when internet access is available,
	then deploy the package into your air-gapped environment.
		`

	getExample = `
		# Run Hauler
		haulerctl bundle images <images>
		haulerctl bundle artifacts <artfiacts>
		haulerctl relocate artifacts <aritfacts>
		haulerctl relocate images <images>
		haulerctl copy
		haulerctl create
		haulerctl bootstrap`
)

// NewRootCommand defines the root haulerctl command
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "haulerctl",
		Short:        "haulerctl provides CLI-based air-gap migration assistance",
		Long:         getLong,
		Example:      getExample,
		SilenceUsage: true,
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

	cmd.AddCommand(NewRelocateCommand())
	cmd.AddCommand(NewCreateCommand())
	cmd.AddCommand(NewBundleCommand())
	cmd.AddCommand(NewCopyCommand())
	cmd.AddCommand(NewBootstrapCommand())

	f := cmd.PersistentFlags()
	f.StringVarP(&loglevel, "loglevel", "l", "info",
		"Log level (debug, info, warn, error, fatal, panic)")
	f.StringVarP(&cfgFile, "config", "c", "./hauler.yaml",
		"config file (./hauler.yaml)")
	f.DurationVar(&timeout, "timeout", 1*time.Minute,
		"timeout for operations")

	return cmd
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".hauler" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".hauler")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
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
