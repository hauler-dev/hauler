package store

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/store"
)

type InfoOpts struct{}

func (o *InfoOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f

	// TODO: Regex matching
}

func InfoCmd(ctx context.Context, o *InfoOpts, s *store.Store) error {
	refs, err := s.List(ctx)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "Reference\tTag/Digest\tType\n")
	fmt.Fprintf(tw, "---------\t----------\t----\n")
	for _, r := range refs {
		if _, ok := r.Annotations[ocispec.AnnotationRefName]; !ok {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\n", r.Annotations[ocispec.AnnotationRefName], "")
	}

	return nil
}
