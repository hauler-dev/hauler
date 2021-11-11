package download

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/artifact/types"
	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct {
	DestinationDir string
	OutputFile     string
}

func (o *Opts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
	f.StringVarP(&o.OutputFile, "output", "o", "", "(Optional) Override name of file to save.")
}

func Cmd(ctx context.Context, o *Opts, reference string) error {
	lgr := log.FromContext(ctx)
	lgr.Debugf("running command `hauler download`")

	cs := content.NewFileStore(o.DestinationDir)
	defer cs.Close()

	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}

	// resolver := docker.NewResolver(docker.ResolverOptions{})

	desc, err := remote.Get(ref)
	if err != nil {
		return err
	}

	manifestData, err := desc.RawManifest()
	if err != nil {
		return err
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}

	// TODO: These need to be factored out into each of the contents own logic
	switch manifest.Config.MediaType {
	case types.DockerConfigJSON, types.OCIManifestSchema1:
		lgr.Infof("identified [image] (%s) content", manifest.Config.MediaType)
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		outputFile := o.OutputFile
		if outputFile == "" {
			outputFile = fmt.Sprintf("%s:%s.tar", path.Base(ref.Context().RepositoryStr()), ref.Identifier())
		}

		if err := tarball.WriteToFile(outputFile, ref, img); err != nil {
			return err
		}

		lgr.Infof("downloaded [%s] to [%s]", ref.Name(), outputFile)

	case types.FileConfigMediaType:
		lgr.Infof("identified [file] (%s) content", manifest.Config.MediaType)

		fs := content.NewFileStore(o.DestinationDir)

		resolver := docker.NewResolver(docker.ResolverOptions{})
		mdesc, descs, err := oras.Pull(ctx, resolver, ref.Name(), fs)
		if err != nil {
			return err
		}

		lgr.Infof("downloaded [%d] files with digest [%s]", len(descs), mdesc)

	case types.ChartLayerMediaType, types.ChartConfigMediaType:
		lgr.Infof("identified [chart] (%s) content", manifest.Config.MediaType)

		fs := content.NewFileStore(o.DestinationDir)

		resolver := docker.NewResolver(docker.ResolverOptions{})
		mdesc, _, err := oras.Pull(ctx, resolver, ref.Name(), fs)
		if err != nil {
			return err
		}

		lgr.Infof("downloaded chart [%s] with digest [%s]", "donkey", mdesc.Digest.String())

	default:
		return fmt.Errorf("unrecognized content type: %s", manifest.Config.MediaType)
	}

	return nil
}

func getManifest(ctx context.Context, ref string) (*remote.Descriptor, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parsing reference %q: %v", ref, err)
	}

	desc, err := remote.Get(r, remote.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	return desc, nil
}
