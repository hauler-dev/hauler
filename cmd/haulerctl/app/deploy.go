package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/deployer"
	"github.com/rancherfederal/hauler/pkg/kube"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
	"time"
)

type deployOpts struct {
	haul string
}

func NewDeployCommand() *cobra.Command {
	opts := &deployOpts{}

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy all dependencies from a generated package.

Given an archive generated from the package command, deploy all needed
components to serve packaged dependencies.`,
		Aliases: []string{"d", "dpl", "dep"},
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	return cmd
}

// Run performs the operation.
func (o *deployOpts) Run(haul string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dpl := deployer.NewDeployer()
	if err := dpl.Deploy(ctx, haul); err != nil {
		return err
	}

	err := waitForReady()
	if err != nil {
		return err
	}

	return nil
}

// waitForReady will wait for the cluster components to be ready
// TODO: Make components dynamic based on what is auto-loaded
func waitForReady() error {
	cfg, err := kube.NewKubeClientConfig()
	if err != nil {
		return err
	}

	checker, err := kube.NewStatusChecker(cfg, 5*time.Second, 30*time.Minute)
	if err != nil {
		return err
	}

	objs, err := buildComponentRefs()

	if err := checker.WaitForCondition(objs...); err != nil {
		return err
	}

	return nil
}

func buildComponentRefs() ([]object.ObjMetadata, error) {
	var objRefs []object.ObjMetadata
	for _, deployment := range []string{"coredns", "local-path-provisioner"} {
		objMeta, err := object.CreateObjMetadata("kube-system", deployment, schema.GroupKind{Group: "apps", Kind: "Deployment"})
		if err != nil {
			return nil, err
		}
		objRefs = append(objRefs, objMeta)
	}
	return objRefs, nil
}