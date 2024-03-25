package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/olekukonko/tablewriter"
	"os"
	"sort"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/consts"

	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/pkg/reference"
)

type InfoOpts struct {
	*RootOpts

	OutputFormat string
	TypeFilter   string
	SizeUnit     string
}

func (o *InfoOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.OutputFormat, "output", "o", "table", "Output format (table, json)")
	f.StringVarP(&o.TypeFilter, "type", "t", "all", "Filter on type (image, chart, file, sigs, atts, sbom)")

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

		// handle multi-arch images
		if desc.MediaType == consts.OCIImageIndexSchema || desc.MediaType == consts.DockerManifestListSchema2 {
			var idx ocispec.Index
			if err := json.NewDecoder(rc).Decode(&idx); err != nil {
				return err
			}

			for _, internalDesc := range idx.Manifests {
				rc, err := s.Fetch(ctx, internalDesc)
				if err != nil {
					return err
				}
				defer rc.Close()

				var internalManifest ocispec.Manifest
				if err := json.NewDecoder(rc).Decode(&internalManifest); err != nil {
					return err
				}

				i := newItem(desc, internalManifest, fmt.Sprintf("%s/%s", internalDesc.Platform.OS, internalDesc.Platform.Architecture), o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			}
			// handle "non" multi-arch images
		} else if desc.MediaType == consts.DockerManifestSchema2 || desc.MediaType == consts.OCIManifestSchema1 {
			var m ocispec.Manifest
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return err
			}

			rc, err := s.FetchManifest(ctx, m)
			if err != nil {
				return err
			}
			defer rc.Close()

			// Unmarshal the OCI image content
			var internalManifest ocispec.Image
			if err := json.NewDecoder(rc).Decode(&internalManifest); err != nil {
				return err
			}

			if internalManifest.Architecture != "" {
				i := newItem(desc, m, fmt.Sprintf("%s/%s", internalManifest.OS, internalManifest.Architecture), o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			} else {
				i := newItem(desc, m, "-", o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			}
			// handle the rest
		} else {
			var m ocispec.Manifest
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return err
			}

			i := newItem(desc, m, "-", o)
			var emptyItem item
			if i != emptyItem {
				items = append(items, i)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	// sort items by ref and arch
	sort.Sort(byReferenceAndArch(items))

	var msg string
	switch o.OutputFormat {
	case "json":
		msg = buildJson(items...)
		fmt.Println(msg)
	default:
		buildTable(items...)
	}
	return nil
}

func buildTable(items ...item) {
	// Create a table for the results
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Reference", "Type", "Platform", "# Layers", "Size"})
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetRowLine(false)
	table.SetAutoMergeCellsByColumnIndex([]int{0})

	totalSize := int64(0)
	for _, i := range items {
		if i.Type != "" {
			row := []string{
				i.Reference,
				i.Type,
				i.Platform,
				fmt.Sprintf("%d", i.Layers),
				byteCountSI(i.Size),
			}
			totalSize += i.Size
			table.Append(row)
		}
	}
	table.SetFooter([]string{"", "", "", "Total", byteCountSI(totalSize)})

	table.Render()
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
	Platform  string
	Layers    int
	Size      int64
}

type byReferenceAndArch []item

func (a byReferenceAndArch) Len() int      { return len(a) }
func (a byReferenceAndArch) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byReferenceAndArch) Less(i, j int) bool {
	if a[i].Reference == a[j].Reference {
		if a[i].Type == "image" && a[j].Type == "image" {
			return a[i].Platform < a[j].Platform
		}
		if a[i].Type == "image" {
			return true
		}
		if a[j].Type == "image" {
			return false
		}
		return a[i].Type < a[j].Type
	}
	return a[i].Reference < a[j].Reference
}

func newItem(desc ocispec.Descriptor, m ocispec.Manifest, plat string, o *InfoOpts) item {
	var size int64 = 0
	for _, l := range m.Layers {
		size += l.Size
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

	switch desc.Annotations["kind"] {
	case "dev.cosignproject.cosign/sigs":
		ctype = "sigs"
	case "dev.cosignproject.cosign/atts":
		ctype = "atts"
	case "dev.cosignproject.cosign/sboms":
		ctype = "sbom"
	}

	ref, err := reference.Parse(desc.Annotations[ocispec.AnnotationRefName])
	if err != nil {
		return item{}
	}

	if o.TypeFilter != "all" && ctype != o.TypeFilter {
		return item{}
	}

	return item{
		Reference: ref.Name(),
		Type:      ctype,
		Platform:  plat,
		Layers:    len(m.Layers),
		Size:      size,
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
