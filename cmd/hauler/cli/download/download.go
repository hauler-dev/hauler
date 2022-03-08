package download

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	"github.com/rancherfederal/hauler/pkg/version"
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

func Cmd(ctx context.Context, o *Opts, reference string) error {
	l := log.FromContext(ctx)

	cs := content.NewFileStore(o.DestinationDir)
	defer cs.Close()

	// build + configure oras client
	var refOpts []name.Option
	remoteOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	if o.PlainHTTP {
		refOpts = append(refOpts, name.Insecure)
	}

	if o.Username != "" || o.Password != "" {
		basicAuth := &authn.Basic{
			Username: o.Username,
			Password: o.Password,
		}
		remoteOpts = append(remoteOpts, remote.WithAuth(basicAuth))
	}

	if o.Insecure {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true

		remoteOpts = append(remoteOpts, remote.WithTransport(transport))
	}

	// build + configure containerd client
	var registryOpts []docker.RegistryOpt

	if o.PlainHTTP {
		registryOpts = append(registryOpts, docker.WithPlainHTTP(docker.MatchAllHosts))
	}

	if o.Username != "" || o.Password != "" {
		creds := func(string) (string, string, error) {
			return o.Username, o.Password, nil
		}
		authorizer := docker.NewDockerAuthorizer(docker.WithAuthCreds(creds))
		registryOpts = append(registryOpts, docker.WithAuthorizer(authorizer))
	}

	if o.Insecure {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true

		httpClient := &http.Client{
			Transport: transport,
		}
		registryOpts = append(registryOpts, docker.WithClient(httpClient))
	}

	resolverOpts := docker.ResolverOptions{
		Hosts:   docker.ConfigureDefaultRegistries(registryOpts...),
		Headers: http.Header{},
	}
	resolverOpts.Headers.Set("User-Agent", "hauler/"+version.GitVersion)

	resolver := docker.NewResolver(resolverOpts)

	// begin dowloading target
	ref, err := name.ParseReference(reference, refOpts...)
	if err != nil {
		return err
	}

	desc, err := remote.Get(ref, remoteOpts...)
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
		l.Debugf("identified [image] (%s) content", manifest.Config.MediaType)
		img, err := remote.Image(ref, remoteOpts...)
		if err != nil {
			return err
		}

		outputFile := fmt.Sprintf("%s_%s.tar", path.Base(ref.Context().RepositoryStr()), ref.Identifier())

		if err := tarball.WriteToFile(outputFile, ref, img); err != nil {
			return err
		}

		l.Infof("downloaded image [%s] to [%s]", ref.Name(), outputFile)

	case types.FileConfigMediaType:
		l.Debugf("identified [file] (%s) content", manifest.Config.MediaType)

		fs := content.NewFileStore(o.DestinationDir)

		// TODO - additional accepted media types
		_, descs, err := oras.Pull(ctx, resolver, ref.Name(), fs)
		if err != nil {
			return err
		}

		ldescs := len(descs)
		for i, desc := range descs {
			// NOTE: This is safe without a map key check b/c we're not allowing unnamed content from oras.Pull
			l.Infof("downloaded (%d/%d) files to [%s]", i+1, ldescs, desc.Annotations[ocispec.AnnotationTitle])
		}

	case types.ChartLayerMediaType, types.ChartConfigMediaType:
		l.Debugf("identified [chart] (%s) content", manifest.Config.MediaType)

		fs := content.NewFileStore(o.DestinationDir)

		// TODO - additional accepted media types
		_, descs, err := oras.Pull(ctx, resolver, ref.Name(), fs)
		if err != nil {
			return err
		}

		cn := path.Base(ref.Name())
		for _, d := range descs {
			if n, ok := d.Annotations[ocispec.AnnotationTitle]; ok {
				cn = n
			}
		}

		l.Infof("downloaded chart [%s] to [%s]", ref.String(), cn)

	default:
		return fmt.Errorf("unrecognized content type: %s", manifest.Config.MediaType)
	}

	return nil
}
