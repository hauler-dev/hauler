package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/olekukonko/tablewriter"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func InfoCmd(ctx context.Context, o *flags.InfoOpts, s *store.Layout) error {
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

				i := newItemWithDigest(
					s,
					internalDesc.Digest.String(),
					desc,
					internalManifest,
					fmt.Sprintf("%s/%s", internalDesc.Platform.OS, internalDesc.Platform.Architecture),
					o,
				)
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

			// unmarshal the oci image content
			var internalManifest ocispec.Image
			if err := json.NewDecoder(rc).Decode(&internalManifest); err != nil {
				return err
			}

			if internalManifest.Architecture != "" {
				i := newItem(s, desc, m,
					fmt.Sprintf("%s/%s", internalManifest.OS, internalManifest.Architecture), o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			} else {
				i := newItem(s, desc, m, "-", o)
				var emptyItem item
				if i != emptyItem {
					items = append(items, i)
				}
			}

			// handle everything else (charts, files, sigs, etc.)
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

	if o.ListRepos {
		buildListRepos(items...)
		return nil
	}

	// sort items by ref and arch
	sort.Sort(byReferenceAndArch(items))

	var msg string
	switch o.OutputFormat {
	case "json":
		msg = buildJson(items...)
		fmt.Println(msg)
	default:
		buildTable(o.ShowDigests, items...)
	}
	return nil
}

func buildListRepos(items ...item) {
	// create map to track unique repository names
	repos := make(map[string]bool)

	for _, i := range items {
		repoName := ""
		for j := 0; j < len(i.Reference); j++ {
			if i.Reference[j] == '/' {
				repoName = i.Reference[:j]
				break
			}
		}
		if repoName == "" {
			repoName = i.Reference
		}
		repos[repoName] = true
	}

	// collect and print unique repository names
	for repoName := range repos {
		fmt.Println(repoName)
	}
}

func buildTable(showDigests bool, items ...item) {
	table := tablewriter.NewWriter(os.Stdout)

	if showDigests {
		table.SetHeader([]string{"Reference", "Type", "Platform", "Digest", "# Layers", "Size"})
	} else {
		table.SetHeader([]string{"Reference", "Type", "Platform", "# Layers", "Size"})
	}

	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetRowLine(false)
	table.SetAutoMergeCellsByColumnIndex([]int{0})

	totalSize := int64(0)

	for _, i := range items {
		if i.Type == "" {
			continue
		}

		ref := truncateReference(i.Reference)
		var row []string

		if showDigests {
			digest := i.Digest
			if digest == "" {
				digest = "-"
			}
			row = []string{
				ref,
				i.Type,
				i.Platform,
				digest,
				fmt.Sprintf("%d", i.Layers),
				byteCountSI(i.Size),
			}
		} else {
			row = []string{
				ref,
				i.Type,
				i.Platform,
				fmt.Sprintf("%d", i.Layers),
				byteCountSI(i.Size),
			}
		}

		totalSize += i.Size
		table.Append(row)
	}

	// align total column based on digest visibility
	if showDigests {
		table.SetFooter([]string{"", "", "", "", "Total", byteCountSI(totalSize)})
	} else {
		table.SetFooter([]string{"", "", "", "Total", byteCountSI(totalSize)})
	}

	table.Render()
}

// truncateReference shortens the digest of a reference
func truncateReference(ref string) string {
	const prefix = "@sha256:"
	idx := strings.Index(ref, prefix)
	if idx == -1 {
		return ref
	}
	if len(ref) > idx+len(prefix)+12 {
		return ref[:idx+len(prefix)+12] + "…"
	}
	return ref
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
	Digest    string
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

// overrides the digest with a specific per platform digest
func newItemWithDigest(s *store.Layout, digestStr string, desc ocispec.Descriptor, m ocispec.Manifest, plat string, o *flags.InfoOpts) item {
	item := newItem(s, desc, m, plat, o)
	item.Digest = digestStr
	return item
}

func newItem(s *store.Layout, desc ocispec.Descriptor, m ocispec.Manifest, plat string, o *flags.InfoOpts) item {
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

	refName := desc.Annotations["io.containerd.image.name"]
	if refName == "" {
		refName = desc.Annotations[ocispec.AnnotationRefName]
	}
	ref, err := reference.Parse(refName)
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
		Digest:    desc.Digest.String(),
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
