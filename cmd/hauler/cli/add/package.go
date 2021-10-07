package add

import (
	"context"
	"os"

	"github.com/rancher/wrangler/pkg/yaml"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type PackageOpts struct{}

func (o *PackageOpts) AddFlags(cmd *cobra.Command) {}

func PackageCmd(ctx context.Context, o *PackageOpts, s *store.Store, packageRefs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler add package`")

	l.With(log.Fields{"dir": s.DataDir}).Debugf("Opening store")
	s.Start()
	defer s.Stop()

	var pkgs []v1alpha1.Package
	for _, packageRef := range packageRefs {
		l.Infof("Loading package from: %s", packageRef)
		pkg, err := loadPackage(packageRef)
		if err != nil {
			return err
		}

		pkgs = append(pkgs, pkg)
	}

	for _, pkg := range pkgs {
		l.Infof("Building package: %s", pkg.Name)
		p, err := content.NewPackage(ctx, pkg)
		if err != nil {
			return err
		}

		l.Infof("Adding package and dependencies to store")
		if err = s.Add(ctx, p); err != nil {
			return err
		}
	}

	l.With(log.Fields{"dir": s.DataDir}).Debugf("Closing store")
	return nil
}

func loadPackage(path string) (v1alpha1.Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return v1alpha1.Package{}, err
	}

	var bundle v1alpha1.Package
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		return v1alpha1.Package{}, err
	}

	return bundle, nil
}
