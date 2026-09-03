package store

// store_annotation_test.go covers containerd-name normalization in
// writeImage/writeIndex. This file is intentionally `package store`
// (whitebox) rather than `package store_test` because writeImage is
// unexported.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/reference"
)

func TestWriteImageNormalizesContainerdName(t *testing.T) {
	l, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	ref, err := reference.ParseReference("docker.io/library/busybox:v1")
	if err != nil {
		t.Fatalf("ParseReference: %v", err)
	}
	// ref.Name() is "index.docker.io/library/busybox:v1"; the annotation must not be.
	if err := l.writeImage(context.Background(), ref, img, consts.KindAnnotationImage, "", ""); err != nil {
		t.Fatalf("writeImage: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(l.Root, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if len(idx.Manifests) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(idx.Manifests))
	}
	got := idx.Manifests[0].Annotations[consts.ContainerdImageNameKey]
	want := "docker.io/library/busybox:v1"
	if got != want {
		t.Errorf("io.containerd.image.name = %q, want %q", got, want)
	}
}

// TestWriteIndexNormalizesContainerdName is writeIndex's counterpart to
// TestWriteImageNormalizesContainerdName above -- writeImage and writeIndex
// each call href.NormalizeContainerd independently (store.go:460, store.go:543),
// so a fix to one is not evidence the other was updated. This is the
// multi-arch parent-descriptor path #744's digest-pinned index goes through.
func TestWriteIndexNormalizesContainerdName(t *testing.T) {
	l, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	amd64Img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random.Image amd64: %v", err)
	}
	arm64Img, err := random.Image(256, 1)
	if err != nil {
		t.Fatalf("random.Image arm64: %v", err)
	}
	idx := mutate.AppendManifests(
		empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)
	ref, err := reference.ParseReference("docker.io/library/busybox@sha256:" +
		"498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0")
	if err != nil {
		t.Fatalf("ParseReference: %v", err)
	}
	// ref.Name() is "index.docker.io/library/busybox@sha256:..."; the annotation must not be.
	if err := l.writeIndex(context.Background(), ref, idx, consts.KindAnnotationIndex, ""); err != nil {
		t.Fatalf("writeIndex: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(l.Root, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var ociIdx ocispec.Index
	if err := json.Unmarshal(data, &ociIdx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if len(ociIdx.Manifests) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(ociIdx.Manifests))
	}
	got := ociIdx.Manifests[0].Annotations[consts.ContainerdImageNameKey]
	want := "docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"
	if got != want {
		t.Errorf("io.containerd.image.name = %q, want %q", got, want)
	}
	if kind := ociIdx.Manifests[0].Annotations[consts.KindAnnotationName]; kind != consts.KindAnnotationIndex {
		t.Errorf("kind = %q, want %q", kind, consts.KindAnnotationIndex)
	}
}

// TestWriteImageAnnotationRefNameIsRegistryless locks in the counterpart to the
// ContainerdImageNameKey normalization above: writeImage strips the registry off
// AnnotationRefName (store.go:475) so the stored ref.name stays registryless.
// The non-Docker-Hub cases are the ones that matter -- a ghcr/quay/localhost
// image must keep its repository path verbatim and must NOT be mis-attributed to
// Docker Hub or gain a library/ prefix, so that reference.ParseReference reads it
// back to the same registryless identity (see pkg/reference TestParseReference).
func TestWriteImageAnnotationRefNameIsRegistryless(t *testing.T) {
	cases := []struct {
		name           string
		ref            string // as AddImage receives it: fully-qualified via goname on the pull path
		wantRefName    string // registryless org.opencontainers.image.ref.name
		wantContainerd string
	}{
		{
			name:           "docker hub official image keeps library/ but drops registry",
			ref:            "index.docker.io/library/busybox:v1",
			wantRefName:    "library/busybox:v1",
			wantContainerd: "docker.io/library/busybox:v1",
		},
		{
			name:           "ghcr image is not mis-attributed to docker hub",
			ref:            "ghcr.io/org/img:v1",
			wantRefName:    "org/img:v1",
			wantContainerd: "ghcr.io/org/img:v1",
		},
		{
			name:           "quay image is not mis-attributed to docker hub",
			ref:            "quay.io/org/app:2.0",
			wantRefName:    "org/app:2.0",
			wantContainerd: "quay.io/org/app:2.0",
		},
		{
			name:           "registry with port drops only the host",
			ref:            "localhost:5000/team/app:dev",
			wantRefName:    "team/app:dev",
			wantContainerd: "localhost:5000/team/app:dev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := NewLayout(t.TempDir())
			if err != nil {
				t.Fatalf("NewLayout: %v", err)
			}
			img, err := random.Image(256, 1)
			if err != nil {
				t.Fatalf("random.Image: %v", err)
			}
			ref, err := reference.ParseReference(tc.ref)
			if err != nil {
				t.Fatalf("ParseReference: %v", err)
			}
			// containerdName "" mirrors AddImage (store.go), which lets writeImage
			// derive both annotations from ref.
			if err := l.writeImage(context.Background(), ref, img, consts.KindAnnotationImage, "", ""); err != nil {
				t.Fatalf("writeImage: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(l.Root, "index.json"))
			if err != nil {
				t.Fatalf("read index.json: %v", err)
			}
			var idx ocispec.Index
			if err := json.Unmarshal(data, &idx); err != nil {
				t.Fatalf("unmarshal index: %v", err)
			}
			if len(idx.Manifests) != 1 {
				t.Fatalf("expected 1 descriptor, got %d", len(idx.Manifests))
			}
			ann := idx.Manifests[0].Annotations
			if got := ann[ocispec.AnnotationRefName]; got != tc.wantRefName {
				t.Errorf("AnnotationRefName = %q, want %q", got, tc.wantRefName)
			}
			if got := ann[consts.ContainerdImageNameKey]; got != tc.wantContainerd {
				t.Errorf("ContainerdImageNameKey = %q, want %q", got, tc.wantContainerd)
			}
		})
	}
}
