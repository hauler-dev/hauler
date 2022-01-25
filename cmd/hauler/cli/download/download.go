package download

import (
	"context"
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/ocil/pkg/consts"

	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/reference"
)

type Opts struct {
	DestinationDir string

	Username  string
	Password  string
	Insecure  bool
	PlainHTTP bool
}

func (o *Opts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.DestinationDir, "output", "o", "", "Directory to save contents to (defaults to current directory)")
	f.StringVarP(&o.Username, "username", "u", "", "Username when copying to an authenticated remote registry")
	f.StringVarP(&o.Password, "password", "p", "", "Password when copying to an authenticated remote registry")
	f.BoolVar(&o.Insecure, "insecure", false, "Toggle allowing insecure connections when copying to a remote registry")
	f.BoolVar(&o.PlainHTTP, "plain-http", false, "Toggle allowing plain http connections when copying to a remote registry")
}

func Cmd(ctx context.Context, o *Opts, ref string) error {
	l := log.FromContext(ctx)

	ropts := content.RegistryOptions{
		Username:  o.Username,
		Password:  o.Password,
		Insecure:  o.Insecure,
		PlainHTTP: o.PlainHTTP,
	}
	rs, err := content.NewRegistry(ropts)
	if err != nil {
		return err
	}

	r, err := reference.Parse(ref)
	if err != nil {
		return err
	}

	desc, err := remote.Get(r, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
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

	pushedDesc, err := oras.Copy(ctx, rs, r.Name(), mapperStore, "",
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	if err != nil {
		return err
	}

	l.Infof("downloaded [%s] with digest [%s]", pushedDesc.MediaType, pushedDesc.Digest.String())
	return nil
}
