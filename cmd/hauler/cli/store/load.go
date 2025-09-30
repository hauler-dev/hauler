package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// extracts the contents of an archived oci layout to an existing oci layout
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	tempOverride := o.TempOverride

	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}

	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	for _, fileName := range o.FileName {
		l.Infof("loading haul [%s] to [%s]", fileName, o.StoreDir)
		err := unarchiveLayoutTo(ctx, fileName, o.StoreDir, tempDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// accepts an oci layout archive or docker archive, extracts or converts the contents to an existing oci layout, and preserves the index
func unarchiveLayoutTo(ctx context.Context, haulPath string, dest string, tempDir string) error {
	l := log.FromContext(ctx)

	// detect if archive is remote and download it
	if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
		l.Debugf("detected remote archive... starting download... [%s]", haulPath)

		h := getter.NewHttp()
		parsedURL, err := url.Parse(haulPath)
		if err != nil {
			return err
		}
		rc, err := h.Open(ctx, parsedURL)
		if err != nil {
			return err
		}
		defer rc.Close()

		fileName := h.Name(parsedURL)
		if fileName == "" {
			fileName = filepath.Base(parsedURL.Path)
		}
		haulPath = filepath.Join(tempDir, fileName)

		out, err := os.Create(haulPath)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err = io.Copy(out, rc); err != nil {
			return err
		}
		l.Debugf("downloaded remote archive to [%s]", haulPath)
	}

	// detect oci layout archive vs docker archive and convert to oci layout in tempDir
	opener := func() (io.ReadCloser, error) { return os.Open(haulPath) }
	l.Debugf("attempting to inspect [%s] as a docker archive tarball", haulPath)
	if manifests, err := tarball.LoadManifest(opener); err == nil && len(manifests) > 0 {
		l.Debugf("detected docker archive formatted tarball in [%s]", haulPath)
		l.Infof("attempting to convert docker archive to oci layout...")

		m := manifests[0]
		if len(m.RepoTags) == 0 {
			return fmt.Errorf("docker archive has no RepoTags; cannot determine ref")
		}
		repoTag := m.RepoTags[0]
		tag, err := name.NewTag(repoTag)
		if err != nil {
			return fmt.Errorf("invalid docker ref %q: %w", repoTag, err)
		}

		img, err := tarball.ImageFromPath(haulPath, &tag)
		if err != nil {
			return fmt.Errorf("failed loading image from docker archive: %w", err)
		}

		// create the empty oci layout and append the image with the reference name annotation
		p, err := layout.Write(tempDir, empty.Index)
		if err != nil {
			return fmt.Errorf("failed to create empty OCI layout: %w", err)
		}
		if err := p.AppendImage(img, layout.WithAnnotations(map[string]string{
			ocispec.AnnotationRefName: tag.String(),
		})); err != nil {
			return fmt.Errorf("failed appending image to OCI layout: %w", err)
		}

		l.Infof("docker archive to oci layout conversion complete for [%s]", tag.String())
	} else {
		// // for oci layout archive... continue to unpack it
		if err := archives.Unarchive(ctx, haulPath, tempDir); err != nil {
			return err
		}
	}

	// ensure and normalize annotations in the incoming index.json
	data, err := os.ReadFile(filepath.Join(tempDir, "index.json"))
	if err != nil {
		return err
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}

	for i := range idx.Manifests {
		if idx.Manifests[i].Annotations == nil {
			idx.Manifests[i].Annotations = make(map[string]string)
		}
		if _, exists := idx.Manifests[i].Annotations[consts.KindAnnotationName]; !exists {
			idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.KindAnnotationImage
		}
		if ref, ok := idx.Manifests[i].Annotations[consts.ContainerdImageNameKey]; ok {
			if slash := strings.Index(ref, "/"); slash != -1 {
				ref = ref[slash+1:]
			}
			if idx.Manifests[i].Annotations[consts.ImageRefKey] != ref {
				idx.Manifests[i].Annotations[consts.ImageRefKey] = ref
			}
		}
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "index.json"), out, 0o644); err != nil {
		return err
	}

	// copy the valid temp oci layout into the destination content store
	s, err := store.NewLayout(tempDir)
	if err != nil {
		return err
	}

	ts, err := content.NewOCI(dest)
	if err != nil {
		return err
	}

	_, err = s.CopyAll(ctx, ts, nil)
	return err
}
