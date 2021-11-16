package store

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/store"
)

type ListOpts struct{}

func (o *ListOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f

	// TODO: Regex matching
}

func ListCmd(ctx context.Context, o *ListOpts, s *store.Store) error {
	s.Open()
	defer s.Close()

	refs, err := s.List(ctx)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "Reference\tTag/Digest\n")
	for _, r := range refs {
		ref, err := name.ParseReference(r, name.WithDefaultRegistry(""))
		if err != nil {
			return err
		}

		fmt.Fprintf(tw, "%s\t%s\n", ref.Context().String(), ref.Identifier())
	}

	return nil
}
