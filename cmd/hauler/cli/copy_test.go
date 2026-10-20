package cli

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/v2/internal/flags"
)

// newCopyTestRegistry starts an in-memory plain-HTTP OCI registry and returns
// its host:port. Shut down via t.Cleanup.
func newCopyTestRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// TestCopyImageCmd exercises the real copy path (CopyImageCmd -> copyOnce)
// registry-to-registry, with no store involved, asserting the destination
// ends up with the exact digest that was copied.
func TestCopyImageCmd(t *testing.T) {
	src := newCopyTestRegistry(t)
	dst := newCopyTestRegistry(t)
	ro := &flags.CliRootOpts{AuditLevel: "none"}

	t.Run("single image", func(t *testing.T) {
		img, err := random.Image(512, 2)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := img.Digest()
		srcRef, _ := name.NewTag(src+"/app:v1", name.Insecure)
		if err := remote.Write(srcRef, img); err != nil {
			t.Fatalf("seed source: %v", err)
		}

		o := &flags.ImageCopyOpts{PlainHTTP: true}
		if err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:v1", ro); err != nil {
			t.Fatalf("CopyImageCmd: %v", err)
		}

		dstRef, _ := name.NewTag(dst+"/app:v1", name.Insecure)
		got, err := remote.Get(dstRef)
		if err != nil {
			t.Fatalf("get destination: %v", err)
		}
		if got.Digest.String() != want.String() {
			t.Errorf("destination digest = %s, want %s", got.Digest, want)
		}
	})

	t.Run("multi-arch index copied whole", func(t *testing.T) {
		idx, err := random.Index(512, 2, 3)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := idx.Digest()
		srcRef, _ := name.NewTag(src+"/multi:v1", name.Insecure)
		if err := remote.WriteIndex(srcRef, idx); err != nil {
			t.Fatalf("seed source: %v", err)
		}

		o := &flags.ImageCopyOpts{PlainHTTP: true}
		if err := CopyImageCmd(context.Background(), o, src+"/multi:v1", dst+"/multi:v1", ro); err != nil {
			t.Fatalf("CopyImageCmd: %v", err)
		}

		dstRef, _ := name.NewTag(dst+"/multi:v1", name.Insecure)
		got, err := remote.Get(dstRef)
		if err != nil {
			t.Fatalf("get destination: %v", err)
		}
		if got.Digest.String() != want.String() {
			t.Errorf("destination index digest = %s, want %s", got.Digest, want)
		}
	})

	t.Run("platform flag parsed and copied", func(t *testing.T) {
		img, err := random.Image(512, 2)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := img.Digest()
		srcRef, _ := name.NewTag(src+"/plat:v1", name.Insecure)
		if err := remote.Write(srcRef, img); err != nil {
			t.Fatalf("seed source: %v", err)
		}

		o := &flags.ImageCopyOpts{PlainHTTP: true, Platform: "linux/amd64"}
		if err := CopyImageCmd(context.Background(), o, src+"/plat:v1", dst+"/plat:v1", ro); err != nil {
			t.Fatalf("CopyImageCmd: %v", err)
		}

		dstRef, _ := name.NewTag(dst+"/plat:v1", name.Insecure)
		got, err := remote.Get(dstRef)
		if err != nil {
			t.Fatalf("get destination: %v", err)
		}
		if got.Digest.String() != want.String() {
			t.Errorf("destination digest = %s, want %s", got.Digest, want)
		}
	})
}
