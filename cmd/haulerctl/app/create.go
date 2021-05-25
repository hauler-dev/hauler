package app

import (
	"bytes"
	"context"

	"github.com/imdario/mergo"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/packager"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/util/json"
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
		Use:     "create",
		Short:   "Generate a customized artifact containing all dependencies for a bootstrap run",
		Aliases: []string{"cr"},
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cluster := v1alpha1.NewDefaultCluster(o.driver)

	// Merge user defined config with default config
	// TODO: This should be done with types... but we'll need mergo for more stuff so lazy approach here
	if err := mergo.Merge(cluster, o.userClusterConfig, mergo.WithOverride); err != nil {
		return err
	}

	d, _ := json.Marshal(cluster)
	buf := bytes.NewReader(d)
	viper.ReadConfig(buf)

	pkg := packager.NewPackager(cluster)
	if err := pkg.Package(ctx, o.outputFile); err != nil {
		return err
	}

	return nil
}
