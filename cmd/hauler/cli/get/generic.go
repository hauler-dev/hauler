package get

import (
	"context"
	"os"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

type GenericOpts struct {
	DestinationDir string
}

func (o *GenericOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func GenericCmd(ctx context.Context, o *GenericOpts, refs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler get generic`")

	store := content.NewFileStore("")
	defer store.Close()

	resolver := docker.NewResolver(docker.ResolverOptions{})

	var dir string
	if o.DestinationDir == "" {
		// Default to current directory
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd

	} else {
		// Ensure directory exists and we can write to it
		_, err := os.Stat(o.DestinationDir)
		if !os.IsNotExist(err) && err != nil {
			return err
		}

		if err := os.MkdirAll(o.DestinationDir, os.ModePerm); err != nil {
			return err
		}

		dir = o.DestinationDir
	}

	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(pwd)
	if err := os.Chdir(dir); err != nil {
		return err
	}

	for _, ref := range refs {
		desc, _, err := oras.Pull(ctx, resolver, ref, store)
		if err != nil {
			return err
		}

		l.Infof("Fetched %s with %s", ref, desc.Digest.String())
	}

	return nil
}
