package download

import (
	"context"
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/consts"
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
	l := log.FromContext(ctx)

	rs, err := content.NewRegistry(content.RegistryOptions{})
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}

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

	mapperStore, err := mapper.FromManifest(manifest, o.DestinationDir)
	if err != nil {
		return err
	}

	pushedDesc, err := oras.Copy(ctx, rs, ref.Name(), mapperStore, "",
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	if err != nil {
		return err
	}

	l.Infof("downloaded [%s] with digest [%s]", pushedDesc.MediaType, pushedDesc.Digest.String())
	return nil
}
