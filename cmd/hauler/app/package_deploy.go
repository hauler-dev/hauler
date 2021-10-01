package app

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/distribution/distribution/v3/reference"
	"github.com/distribution/distribution/v3/registry/client"
	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"
	ocontent "oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content"
)

type packageDeployOpts struct {
	*rootOpts
	*packageOpts

	// Inputs
	registryBaseURL string
	packagePaths    []string
	repo            string
	kubeconfigPath  string

	// Generated
	registryURL *url.URL
	packages    []v1alpha1.Package
}

func NewPackageDeployCommand() *cobra.Command {
	opts := &packageDeployOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:     "deploy",
		Aliases: []string{"d"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringSliceVarP(&opts.packagePaths, "packages", "p", []string{}, "Path to hauler created archived package(s), can be specified multiple times")
	f.StringVarP(&opts.registryBaseURL, "registry-url", "u", "http://localhost:5000", "Base path to registry (http://localhost:5000)")
	f.StringVarP(&opts.repo, "repo", "r", "hauler", "Repository name where packages are located (hauler)")

	return cmd
}

func (o *packageDeployOpts) PreRun() error {
	for _, ppath := range o.packagePaths {
		o.logger.Infof("Decompressing and unarchiving %s to %s", ppath, o.datadir)
		a := archiver.NewTarZstd()
		a.OverwriteExisting = true

		err := a.Unarchive(ppath, o.datadir)
		if err != nil {
			return err
		}
	}

	o.logger.Debugf("Parsing %s to canonical url", o.registryBaseURL)
	u, err := url.Parse(o.registryBaseURL)
	if err != nil {
		return err
	}
	o.registryURL = u

	return nil
}

func (o *packageDeployOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build a registry client
	rc, err := client.NewRegistry(o.registryURL.String(), nil)
	if err != nil {
		return err
	}

	repos := make([]string, 10)

	n, err := rc.Repositories(ctx, repos, "")
	if err != nil {
		return err
	}

	o.logger.Infof("Identified %d repositories within %s\n%v", n, o.registryURL.String(), repos)

	repo, _ := reference.WithName(path.Join(content.SystemContentRepo, "k3s"))
	rec, err := client.NewRepository(repo, o.registryURL.String(), nil)
	if err != nil {
		return err
	}

	t := rec.Tags(ctx)
	all, err := t.All(ctx)
	if err != nil {
		return err
	}
	o.logger.Infof("%v", all)

	if err := ociGet(ctx, "localhost:5000/hauler/fleet:v0.3.6"); err != nil {
		return err
	}

	return nil
}

func ociGet(ctx context.Context, ref string) error {
	store := ocontent.NewFileStore("")
	defer store.Close()

	resolver := docker.NewResolver(docker.ResolverOptions{})

	allowedMediaTypes := []string{}
	_ = allowedMediaTypes

	desc, _, err := oras.Pull(ctx, resolver, ref, store)
	if err != nil {
		return err
	}

	fmt.Println(desc)
	return nil
}
