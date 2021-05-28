package app

import (
	"context"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/create"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type createOpts struct {
}

func NewCreateCommand() *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use: "create",
		Short: "create a haul",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o createOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.NewPrettyLogger()

	// TODO: Load this from config if provided
	h := haul.Haul{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Haul",
			APIVersion: "v1alpha1",
		},
		Metadata: metav1.ObjectMeta{
			Name: "haul",
		},
		Spec:     haul.HaulSpec{
			Driver: driver.K3sDriver{
				Version: driver.K3sDefaultVersion,
			},
			PreloadImages: []string{
				"plndr/kube-vip:0.3.4",
				"registry:2.7.1",
				"gitea/gitea:1.14.1-rootless",
			},
		},
	}

	d, err := yaml.Marshal(h)
	if err != nil {
		return err
	}
	fmt.Println(string(d))

	c, err := create.NewCreator(logger)
	if err != nil {
		return err
	}

	_ = c
	_ = ctx
	//if err := c.Create(ctx, h); err != nil {
	//	return err
	//}

	return nil
}