package str

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ListOpts struct{}

func (o *ListOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f

	// TODO: Regex matching
}

func ListCmd(ctx context.Context, o *ListOpts, s *store.Store) error {
	lgr := log.FromContext(ctx)
	lgr.Debugf("running cli command `hauler store list`")

	s.Open()
	defer s.Close()

	refs, err := s.List(ctx)
	if err != nil {
		return err
	}

	// TODO: Just use a tabler library
	tw := tabwriter.NewWriter(os.Stdout, 8, 12, 4, '\t', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "#\tReference\tIdentifier\n")
	for i, r := range refs {
		ref, err := name.ParseReference(r, name.WithDefaultRegistry(""))
		if err != nil {
			return err
		}

		fmt.Fprintf(tw, "%d\t%s\t%s\n", i, ref.Context().String(), ref.Identifier())
	}

	return nil
}
