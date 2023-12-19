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
	f.StringVarP(&o.TypeFilter, "type", "t", "all", "Filter on type (image, chart, file)")

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

				i := newItem(s, desc, internalManifest, internalDesc.Platform.Architecture, o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			}
		// handle single arch docker images
		} else if desc.MediaType == consts.DockerManifestSchema2 {
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

			i := newItem(s, desc, m, internalManifest.Architecture, o)
			var emptyItem item
			if i != emptyItem {
				items = append(items, i)
			}
		// handle the rest
		} else {  
			var m ocispec.Manifest
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return err
			}

			i := newItem(s, desc, m, "-", o)
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
	table.SetHeader([]string{"Reference", "Type", "Arch", "# Layers", "Size"})
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetRowLine(false)
	table.SetAutoMergeCellsByColumnIndex([]int{0})

	for _, i := range items {
		if i.Type != "" {
			row := []string{
				i.Reference,
				i.Type,
				i.Architecture,
				fmt.Sprintf("%d", i.Layers),
				i.Size,
			}
			table.Append(row)
		}
	}
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
	Reference    string
	Type         string
	Architecture string
	Layers       int
	Size         string
}

type byReferenceAndArch []item

func (a byReferenceAndArch) Len() int      { return len(a) }
func (a byReferenceAndArch) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byReferenceAndArch) Less(i, j int) bool {
	if a[i].Reference == a[j].Reference {
		return a[i].Architecture < a[j].Architecture
	}
	return a[i].Reference < a[j].Reference
}

func newItem(s *store.Layout, desc ocispec.Descriptor, m ocispec.Manifest, arch string, o *InfoOpts) item {
	// skip listing cosign items
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

	if o.TypeFilter != "all" && ctype != o.TypeFilter {
		return item{}
	}

	return item{
		Reference:    ref.Name(),
		Type:         ctype,
		Architecture: arch,
		Layers:       len(m.Layers),
		Size:         byteCountSI(size),
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
