package app

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type createOpts struct {
	driver            string
	outputFile        string
	userClusterConfig v1alpha1.Cluster
	clusterConfigFile string
}

// NewCreateCommand creates a new sub command under
// haulerctl  for creating dependency artifacts for bootstraps
func NewCreateCommand() *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "new",
		Short: "package all dependencies into a compressed archive",
		Long: `package all dependencies into a compressed archive used by deploy.

Container images, git repositories, and more, packaged and ready to be served within an air gap.`,
		Aliases: []string{"p", "pkg"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.driver, "driver", "d", "k3s",
		"Driver type to use for package (k3s or rke2)")
	f.StringVarP(&opts.outputFile, "output", "o", "haul.tar.zst",
		"package output location relative to the current directory (haul.tar.zst)")
	f.StringVarP(&opts.clusterConfigFile, "config", "c", "./cluster.yaml",
		"config file to use to override default utility cluster settings (./cluster.yaml)")

	return cmd
}

func (o *createOpts) PreRun() error {
	viper.AutomaticEnv()

	viper.SetConfigFile(o.clusterConfigFile)

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		log.Debugf("Using config file: %s", viper.ConfigFileUsed())
	}

	err := viper.Unmarshal(&o.userClusterConfig)
	if err != nil {
		log.Fatalf("Failed to unmarshal config file: %v", err)
	}

	return nil
}

// Run performs the operation.
func (o *createOpts) Run() error {
	return nil
}
