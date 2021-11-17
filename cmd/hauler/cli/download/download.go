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
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
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

	fs := content.NewFile(o.DestinationDir)
	defer fs.Close()

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

		is := mapper.NewStore(o.DestinationDir, image.Mapper())
		defer is.Close()

		ms = is

		// l.Infof("downloaded image [%s] to [%s]", ref.Name(), outputFile)

	case consts.FileConfigMediaType:
		l.Debugf("identified [file] (%s) content", manifest.Config.MediaType)

		fs := mapper.NewStore(o.DestinationDir, file.Mapper())
		defer fs.Close()

		ms = fs

		// _, err := oras.Copy(ctx, rs, ref.Name(), fs, "",
		// 	oras.WithLayerDescriptors(func(descriptors []ocispec.Descriptor) {
		// 		for _, desc := range descriptors {
		// 			if _, ok := desc.Annotations[ocispec.AnnotationTitle]; !ok {
		// 				continue
		// 			}
		// 			descs = append(descs, desc)
		// 		}
		// 	}))
		// if err != nil {
		// 	return err
		// }
		//
		// ldescs := len(descs)
		// for i, desc := range descs {
		// 	// NOTE: This is safe without a map key check b/c we're not allowing unnamed content from oras.Pull
		// 	l.Infof("downloaded (%d/%d) files to [%s]", i+1, ldescs, desc.Annotations[ocispec.AnnotationTitle])
		// }

	case consts.ChartLayerMediaType, consts.ChartConfigMediaType:
		l.Debugf("identified [chart] (%s) content", manifest.Config.MediaType)

		cs := mapper.NewStore(o.DestinationDir, chart.Mapper())
		defer cs.Close()

		ms = cs
		// desc, err := oras.Copy(ctx, rs, ref.Name(), fs, "")
		// if err != nil {
		// 	return err
		// }
		//
		// l.Infof("downloaded chart [%s] to [%s]", ref.String(), desc.Annotations[ocispec.AnnotationTitle])

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
