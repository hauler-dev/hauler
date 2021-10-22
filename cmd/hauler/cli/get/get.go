package get

import (
	"context"
	"errors"
	"strings"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct {
	DestinationDir string
}

func (o *Opts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func Cmd(ctx context.Context, o *Opts, ref string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler get`")

	store := content.NewFileStore("")
	defer store.Close()

	resolver := docker.NewResolver(docker.ResolverOptions{})

	if !hasRegistry(ref) {
		// Assume we're trying to get something from the local store
		return errors.New("no registry detected in reference. If you're trying to fetch content from hauler's embedded store, please use `hauler store get`")
	}

	l.Infof("Getting %s", ref)
	desc, _, err := oras.Pull(ctx, resolver, ref, store)
	if err != nil {
		return err
	}

	l.Infof("Fetched '%s' of type '%s'", desc.Digest.String(), desc.MediaType)

	return nil
}

func hasRegistry(name string) bool {
	var registry string
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 && (strings.ContainsRune(parts[0], '.') || strings.ContainsRune(parts[0], ':')) {
		// The first part of the repository is treated as the registry domain
		// iff it contains a '.' or ':' character, otherwise it is all repository
		// and the domain defaults to Docker Hub.
		registry = parts[0]
	}

	return registry != ""
}
