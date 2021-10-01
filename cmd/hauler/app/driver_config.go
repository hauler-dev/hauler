package app

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/driver"
)

type driverConfigOpts struct {
	*rootOpts

	// User defined
	kind    string
	version string
}

func NewDriverConfigCommand() *cobra.Command {
	o := driverConfigOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "output bootstrap config for driver",
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.kind, "kind", "k3s", "Kind of driver to package (k3s or rke2)")
	f.StringVarP(&o.version, "version", "v", "", "Version of driver to package")

	return cmd
}

func (o *driverConfigOpts) Run() error {
	logger := o.rootOpts.logger

	logger.Debugf("Initializing driver with kind: %s, version %s", o.kind, o.version)
	d, err := driver.NewDriver(o.kind, o.version)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(os.Stdout, "%s", d.Template())
	if err != nil {
		return err
	}

	return nil
}
