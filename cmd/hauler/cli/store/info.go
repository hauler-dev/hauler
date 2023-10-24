package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/consts"

	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/pkg/reference"
)

type InfoOpts struct {
	*RootOpts

	OutputFormat string
	SizeUnit     string
}

func (o *InfoOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.OutputFormat, "output", "o", "table", "Output format (table, json)")

	// TODO: Regex/globbing
}

func InfoCmd(ctx context.Context, o *InfoOpts, s *store.Layout) error {
	var items []item
	if err := s.Walk(func(ref string, desc ocispec.Descriptor) error {
		if _, ok := desc.Annotations[ocispec.AnnotationRefName]; !ok {
			return nil
		}
		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return err
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return err
		}
		i := newItem(s, desc, m)
		var emptyItem item
		if i != emptyItem {
			items = append(items, i)
		}

		return nil
	}); err != nil {
		return err
	}

	var msg string
	switch o.OutputFormat {
	case "json":
		msg = buildJson(items...)

	default:
		msg = buildTable(items...)
	}
	fmt.Println(msg)
	return nil
}

func buildTable(items ...item) string {
	b := strings.Builder{}
	tw := tabwriter.NewWriter(&b, 1, 1, 3, ' ', 0)

	fmt.Fprintf(tw, "Reference\tType\t# Layers\tSize\n")
	fmt.Fprintf(tw, "---------\t----\t--------\t----\n")

	for _, i := range items {
		if i.Type != "" {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				i.Reference, i.Type, i.Layers, i.Size,
			)
		}
	}
	tw.Flush()
	return b.String()
}

func buildJson(item ...item) string {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

type item struct {
	Reference string
	Type      string
	Layers    int
	Size      string
}

func newItem(s *store.Layout, desc ocispec.Descriptor, m ocispec.Manifest) item {
	if desc.Annotations["kind"] == "dev.cosignproject.cosign/atts" ||
	   desc.Annotations["kind"] == "dev.cosignproject.cosign/sigs" ||
	   desc.Annotations["kind"] == "dev.cosignproject.cosign/sboms" {
		return item{}
	}

	var size int64 = 0
	for _, l := range m.Layers {
		size = +l.Size
	}

	// Generate a human-readable content type
	var ctype string
	switch m.Config.MediaType {
	case consts.DockerConfigJSON:
		ctype = "image"
	case consts.ChartConfigMediaType:
		ctype = "chart"
	case consts.FileLocalConfigMediaType, consts.FileHttpConfigMediaType:
		ctype = "file"
	default:
		ctype = "image"
	}

	ref, err := reference.Parse(desc.Annotations[ocispec.AnnotationRefName])
	if err != nil {
		return item{}
	}

	return item{
		Reference: ref.Name(),
		Type:      ctype,
		Layers:    len(m.Layers),
		Size:      byteCountSI(size),
	}
}

func byteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}
