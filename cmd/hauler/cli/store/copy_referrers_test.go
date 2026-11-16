package store

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// newTargetRegistry serves an in-memory registry with or without the referrers API, the latter like registries without it (i.e. ghcr and distribution).
func newTargetRegistry(t *testing.T, referrersAPI bool) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(registry.New(registry.WithReferrersSupport(referrersAPI)))
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// TestCopyCmd_Registry_LinksReferrers verifies a referrer (i.e. a cosign bundle) copied to a registry is discoverable from its subject, through the fallback tag when the registry lacks the referrers API and through the API when it has one.
func TestCopyCmd_Registry_LinksReferrers(t *testing.T) {
	for _, tc := range []struct {
		name         string
		referrersAPI bool
	}{
		{name: "registry without the referrers api", referrersAPI: false},
		{name: "registry with the referrers api", referrersAPI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext(t)
			srcHost, srcOpts := newLocalhostRegistry(t)

			subject := seedImage(t, srcHost, "test/signed", "v1", srcOpts...)
			subjectDesc, err := partial.Descriptor(subject)
			if err != nil {
				t.Fatal(err)
			}
			bundle := mutate.ConfigMediaType(mutate.MediaType(empty.Image, gvtypes.OCIManifestSchema1), "application/vnd.dev.sigstore.bundle.v0.3+json")
			bundle = mutate.Subject(bundle, *subjectDesc).(gcrv1.Image)
			bundleDigest, err := bundle.Digest()
			if err != nil {
				t.Fatal(err)
			}
			bundleRef, err := goname.NewDigest(srcHost+"/test/signed@"+bundleDigest.String(), goname.Insecure)
			if err != nil {
				t.Fatal(err)
			}
			if err := remote.Write(bundleRef, bundle, srcOpts...); err != nil {
				t.Fatalf("remote.Write bundle: %v", err)
			}

			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()
			if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/signed:v1"}, "", false, rso, ro, "", "", false); err != nil {
				t.Fatalf("storeImage: %v", err)
			}

			dstHost := newTargetRegistry(t, tc.referrersAPI)
			if err := CopyCmd(ctx, &flags.CopyOpts{StoreRootOpts: rso, PlainHTTP: true}, s, "registry://"+dstHost, ro); err != nil {
				t.Fatalf("CopyCmd: %v", err)
			}

			subjectDigest, err := subject.Digest()
			if err != nil {
				t.Fatal(err)
			}
			dstSubject, err := goname.NewDigest(dstHost+"/test/signed@"+subjectDigest.String(), goname.Insecure)
			if err != nil {
				t.Fatal(err)
			}
			idx, err := remote.Referrers(dstSubject)
			if err != nil {
				t.Fatalf("remote.Referrers: %v", err)
			}
			im, err := idx.IndexManifest()
			if err != nil {
				t.Fatal(err)
			}
			if len(im.Manifests) != 1 || im.Manifests[0].Digest != bundleDigest {
				t.Fatalf("expected the bundle [%s] as the only referrer, got %+v", bundleDigest, im.Manifests)
			}

			fallback, err := goname.NewTag(dstHost+"/test/signed:"+strings.ReplaceAll(subjectDigest.String(), ":", "-"), goname.Insecure)
			if err != nil {
				t.Fatal(err)
			}
			_, err = remote.Head(fallback)
			if !tc.referrersAPI && err != nil {
				t.Errorf("expected the fallback tag [%s] on a registry without the referrers api: %v", fallback.TagStr(), err)
			}
			if tc.referrersAPI && err == nil {
				t.Errorf("expected no fallback tag on a registry with the referrers api")
			}
		})
	}
}
