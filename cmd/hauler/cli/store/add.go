package store

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/artifacts/file"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content/chart"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
	"helm.sh/helm/v3/pkg/action"
)

func AddFileCmd(ctx context.Context, o *flags.AddFileOpts, s *store.Layout, reference string) error {
	cfg := v1.File{
		Path: reference,
	}
	if len(o.Name) > 0 {
		cfg.Name = o.Name
	}
	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Layout, fi v1.File) error {
	l := log.FromContext(ctx)

	copts := getter.ClientOptions{
		NameOverride: fi.Name,
	}

	f := file.NewFile(fi.Path, file.WithClient(getter.NewClient(copts)))
	ref, err := reference.NewTagged(f.Name(fi.Path), consts.DefaultTag)
	if err != nil {
		return err
	}

	l.Infof("adding file [%s] to the store as [%s]", fi.Path, ref.Name())
	_, err = s.AddOCI(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added file [%s]", ref.Name())

	return nil
}

func AddImageCmd(ctx context.Context, o *flags.AddImageOpts, s *store.Layout, reference string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	cfg := v1.Image{
		Name:    reference,
		Rewrite: o.Rewrite,
	}

	// Check if the user provided a key.
	if o.Key != "" {
		// verify signature using the provided key.
		err := cosign.VerifySignature(ctx, s, o.Key, o.Tlog, cfg.Name, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("signature verified for image [%s]", cfg.Name)
	} else if o.CertIdentityRegexp != "" || o.CertIdentity != "" {
		// verify signature using the provided keyless details
		l.Infof("verifying keyless signature for [%s]", cfg.Name)
		err := cosign.VerifyKeylessSignature(ctx, s, o.CertIdentity, o.CertIdentityRegexp, o.CertOidcIssuer, o.CertOidcIssuerRegexp, o.CertGithubWorkflowRepository, o.Tlog, cfg.Name, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("keyless signature verified for image [%s]", cfg.Name)
	}

	return storeImage(ctx, s, cfg, o.Platform, rso, ro, o)
}

func storeImage(ctx context.Context, s *store.Layout, i v1.Image, platform string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rw flags.RewriteProvider) error {
	l := log.FromContext(ctx)

	if !ro.IgnoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ro.IgnoreErrors = true
		}
	}

	l.Infof("adding image [%s] to the store", i.Name)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to parse image [%s]: %v... skipping...", i.Name, err)
			return nil
		} else {
			l.Errorf("unable to parse image [%s]: %v", i.Name, err)
			return err
		}
	}

	// copy and sig verification
	err = cosign.SaveImage(ctx, s, r.Name(), platform, rso, ro)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		} else {
			l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
			return err
		}
	}

	if rw.RewriteValue() != "" {
		// rename image name in store
		newRef, err := name.ParseReference(rw.RewriteValue())
		if err != nil {
			l.Errorf("unable to parse rewrite name: %w", err)
		}
		fmt.Println("parsed rewrite: ", newRef.Name()) // remove
		fmt.Println("old reference: ", r.Name())       // remove

		rewriteReference(ctx, s, r, newRef)
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

func rewriteReference(ctx context.Context, s *store.Layout, oldRef name.Reference, newRef name.Reference) error {
	l := log.FromContext(ctx)

	l.Infof("rewriting [%s] to [%s]", oldRef.Name(), newRef.Name())

	s.OCI.LoadIndex()

	//TODO: improve string manipulation
	oldRefContext := oldRef.Context()
	newRefContext := newRef.Context()

	oldRepo := oldRefContext.RepositoryStr()
	newRepo := newRefContext.RepositoryStr()
	oldTag := oldRef.(name.Tag).TagStr()
	newTag := newRef.(name.Tag).TagStr()
	oldRegistry := strings.TrimPrefix(oldRefContext.RegistryStr(), "index.")
	newRegistry := strings.TrimPrefix(newRefContext.RegistryStr(), "index.")

	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag
	oldTotalReg := oldRegistry + "/" + oldTotal
	newTotalReg := newRegistry + "/" + newTotal

	//for debug purposes
	fmt.Println("old repo: ", oldTotal)
	fmt.Println("new repo: ", newTotal)

	fmt.Println("oldRef.Name: ", oldRef.Name())
	fmt.Println("newRef.Name: ", newRef.Name())
	fmt.Println("old registry: ", oldTotalReg)
	fmt.Println("new registry: ", newTotalReg)

	//find and update reference
	found := false
	if err := s.OCI.Walk(func(k string, d ocispec.Descriptor) error {
		if d.Annotations[ocispec.AnnotationRefName] == oldTotal && d.Annotations[consts.ContainerdImageNameKey] == oldTotalReg {
			d.Annotations[ocispec.AnnotationRefName] = newTotal
			d.Annotations[consts.ContainerdImageNameKey] = newTotalReg
			found = true
		}
		return nil
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("could not find image [%s] in store", oldRef.Name())
	}

	return s.OCI.SaveIndex()

}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string) error {
	cfg := v1.Chart{
		Name:    chartName,
		RepoURL: o.ChartOpts.RepoURL,
		Version: o.ChartOpts.Version,
	}

	return storeChart(ctx, s, cfg, o.ChartOpts)
}

func storeChart(ctx context.Context, s *store.Layout, cfg v1.Chart, opts *action.ChartPathOptions) error {
	l := log.FromContext(ctx)

	l.Infof("adding chart [%s] to the store", cfg.Name)

	// TODO: This shouldn't be necessary
	opts.RepoURL = cfg.RepoURL
	opts.Version = cfg.Version

	chrt, err := chart.NewChart(cfg.Name, opts)
	if err != nil {
		return err
	}

	c, err := chrt.Load()
	if err != nil {
		return err
	}

	ref, err := reference.NewTagged(c.Name(), c.Metadata.Version)
	if err != nil {
		return err
	}
	_, err = s.AddOCI(ctx, chrt, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added chart [%s]", ref.Name())
	return nil
}
