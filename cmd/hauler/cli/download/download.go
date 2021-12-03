package download

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"
	"oras.land/oras-go/pkg/target"

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

	var ms target.Target
	// TODO: These need to be factored out into each of the contents own logic
	switch manifest.Config.MediaType {
	case consts.DockerConfigJSON, consts.OCIManifestSchema1:
		l.Debugf("identified [image] (%s) content", manifest.Config.MediaType)

		outputFile := o.OutputFile
		if outputFile == "" {
			outputFile = fmt.Sprintf("%s:%s.tar", path.Base(ref.Context().RepositoryStr()), ref.Identifier())
		}

		s := mapper.NewMapperFileStore(o.DestinationDir, mapper.Images())
		defer s.Close()
		ms = s

	case consts.FileLocalConfigMediaType:
		l.Debugf("identified [file] (%s) content", manifest.Config.MediaType)

		s := mapper.NewMapperFileStore(o.DestinationDir, nil)
		defer s.Close()
		ms = s

	case consts.ChartLayerMediaType, consts.ChartConfigMediaType:
		l.Debugf("identified [chart] (%s) content", manifest.Config.MediaType)

		s := mapper.NewMapperFileStore(o.DestinationDir, mapper.Chart())
		defer s.Close()
		ms = s

	default:
		return fmt.Errorf("unrecognized content type: %s", manifest.Config.MediaType)
	}

	pushedDesc, err := oras.Copy(ctx, rs, ref.Name(), ms, "",
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	if err != nil {
		return err
	}

	l.Infof("downloaded [%s] with digest [%s]", pushedDesc.MediaType, pushedDesc.Digest.String())

	return nil
}
