package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/hauler-dev/hauler/internal/mapper"
	"github.com/hauler-dev/hauler/pkg/log"
	"github.com/hauler-dev/hauler/pkg/reference"
	"github.com/hauler-dev/hauler/pkg/store"
)

type ExtractOpts struct {
	*RootOpts
	DestinationDir string
}

func (o *ExtractOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.DestinationDir, "output", "o", "", "Directory to save contents to (defaults to current directory)")
}

func ExtractCmd(ctx context.Context, o *ExtractOpts, s *store.Layout, ref string) error {
	l := log.FromContext(ctx)

	r, err := reference.Parse(ref)
	if err != nil {
		return err
	}

	found := false
	if err := s.Walk(func(reference string, desc ocispec.Descriptor) error {

		if !strings.Contains(reference, r.Name()) {
			return nil
		}
		found = true

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return err
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return err
		}

		mapperStore, err := mapper.FromManifest(m, o.DestinationDir)
		if err != nil {
			return err
		}

		pushedDesc, err := s.Copy(ctx, reference, mapperStore, "")
		if err != nil {
			return err
		}

		l.Infof("extracted [%s] from store with digest [%s]", pushedDesc.MediaType, pushedDesc.Digest.String())

		return nil
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("reference [%s] not found in store (hint: use `hauler store info` to list store contents)", ref)
	}

	return nil
}
