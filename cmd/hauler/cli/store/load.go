package store

import (
	"context"
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

// LoadCmd extracts the contents of an archived oci layout into an existing store
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

// unarchiveLayoutTo accepts an archived oci layout, extracts the contents to an existing oci layout, and preserves the index
// patches by injecting the cosign metadata, ensuring the oci layout, and updates everything in the store
func unarchiveLayoutTo(ctx context.Context, haulPath, dest, tempDir string) error {
	l := log.FromContext(ctx)

	// detect if archive is a remote file
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

		// create temp file for remote archive
		tempTar, err := os.CreateTemp(tempDir, fileName)
		if err != nil {
			return err
		}
		defer tempTar.Close()
		haulPath = tempTar.Name()

		if _, err = io.Copy(tempTar, rc); err != nil {
			return err
		}
		l.Debugf("downloaded remote archive to [%s]", haulPath)
	}

	opener := func() (io.ReadCloser, error) {
		return os.Open(haulPath)
	}

	// attempt to load the tarball as a docker archive manifest
	l.Debugf("attempting to inspect [%s] as a docker archive tarball", haulPath)
	manifests, err := tarball.LoadManifest(opener)
	// If LoadManifest fails, it's not a valid docker archive so proccess as an oci layout
	if err != nil || len(manifests) == 0 {
		l.Debugf("unable to determine docker archive format... processing as oci layout tarball")
		if err := archives.Unarchive(ctx, haulPath, tempDir); err != nil {
			return err
		}
	} else {
		// If LoadManifest succeeds, it's a valid docker archive
		l.Debugf("detected docker archive formatted tarball in [%s]", haulPath)
		l.Infof("converting docker archive to oci layout...")

		// fetch the tag to identify the image
		if len(manifests[0].RepoTags) == 0 {
			return fmt.Errorf("could not identify the image from the repotags from docker archive tarball")
		}
		repoTag := manifests[0].RepoTags[0]
		tag, err := name.NewTag(repoTag)
		if err != nil {
			return fmt.Errorf("could not parse tag from docker archive manifest [%s]: %w", repoTag, err)
		}

		// load the image from the tarball
		img, err := tarball.ImageFromPath(haulPath, &tag)
		if err != nil {
			return fmt.Errorf("could not load image from docker archive tarball: %w", err)
		}

		// create an empty oci layout in the tempDir
		p, err := layout.Write(tempDir, empty.Index)
		if err != nil {
			return fmt.Errorf("failed to write empty oci layout: %w", err)
		}

		// update the oci layout with the image
		annotations := map[string]string{
			ocispec.AnnotationRefName: tag.String(),
		}
		if err := p.AppendImage(img, layout.WithAnnotations(annotations)); err != nil {
			return fmt.Errorf("failed to append image to oci layout: %w", err)
		}
		l.Infof("successfully converted docker archive to oci layout [%s]", tag.String())
	}

	// patch and mutate the index for cosign metadata and write the oci layout
	l.Debugf("patching metadata in the oci layout in [%s]", tempDir)
	if err := store.EnsureOCILayout(tempDir); err != nil {
		return err
	}

	// load the temporary layout
	s, err := store.NewLayout(tempDir)
	if err != nil {
		return err
	}

	// update store layout with tempDir layout
	ts, err := content.NewOCI(dest)
	if err != nil {
		return err
	}
	if _, err := s.CopyAll(ctx, ts, nil); err != nil {
		return err
	}

	// ensure the store has the layout
	l.Debugf("updating oci layout in store at [%s]", dest)
	return store.EnsureOCILayout(dest)
}
