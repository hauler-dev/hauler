package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/v2/internal/flags"
)

// newCopyTestRegistry starts an in-memory plain http registry and returns its host:port
func newCopyTestRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// TestCopyImageCmd copies registry to registry with no store and checks the destination digest
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

// seedCopyTag pushes a random image to host/repo:tag and returns its digest
func seedCopyTag(t *testing.T, host, repo, tag string) string {
	t.Helper()
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatalf("seed [%s]: %v", ref, err)
	}
	d, _ := img.Digest()
	return d.String()
}

// copyTagDigest returns the digest at host/repo:tag or "" when it does not exist
func copyTagDigest(t *testing.T, host, repo, tag string) string {
	t.Helper()
	ref, _ := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	desc, err := remote.Head(ref)
	if err != nil {
		return ""
	}
	return desc.Digest.String()
}

func TestCopyImageCmdNoClobber(t *testing.T) {
	src := newCopyTestRegistry(t)
	dst := newCopyTestRegistry(t)
	ro := &flags.CliRootOpts{AuditLevel: "none"}

	seedCopyTag(t, src, "app", "v1")
	existing := seedCopyTag(t, dst, "app", "v1")

	o := &flags.ImageCopyOpts{PlainHTTP: true, NoClobber: true, Retries: 1}
	err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:v1", ro)
	if err == nil || !strings.Contains(err.Error(), "--no-clobber") {
		t.Fatalf("expected a --no-clobber error, got %v", err)
	}
	if got := copyTagDigest(t, dst, "app", "v1"); got != existing {
		t.Errorf("destination digest = %s, want untouched %s", got, existing)
	}

	// a tag that does not exist yet copies normally
	want := seedCopyTag(t, src, "app", "v2")
	if err := CopyImageCmd(context.Background(), o, src+"/app:v2", dst+"/app:v2", ro); err != nil {
		t.Fatalf("CopyImageCmd: %v", err)
	}
	if got := copyTagDigest(t, dst, "app", "v2"); got != want {
		t.Errorf("destination digest = %s, want %s", got, want)
	}
}

func TestCopyImageCmdAllTags(t *testing.T) {
	ro := &flags.CliRootOpts{AuditLevel: "none"}

	t.Run("copies every tag", func(t *testing.T) {
		src := newCopyTestRegistry(t)
		dst := newCopyTestRegistry(t)
		want := map[string]string{}
		for _, tag := range []string{"v1", "v2", "v3"} {
			want[tag] = seedCopyTag(t, src, "app", tag)
		}

		o := &flags.ImageCopyOpts{PlainHTTP: true, AllTags: true, Jobs: 2}
		if err := CopyImageCmd(context.Background(), o, src+"/app", dst+"/app", ro); err != nil {
			t.Fatalf("CopyImageCmd: %v", err)
		}

		dstRepo, _ := name.NewRepository(dst+"/app", name.Insecure)
		tags, err := remote.List(dstRepo)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(tags)
		if strings.Join(tags, ",") != "v1,v2,v3" {
			t.Fatalf("destination tags = %v, want [v1 v2 v3]", tags)
		}
		for tag, d := range want {
			if got := copyTagDigest(t, dst, "app", tag); got != d {
				t.Errorf("tag [%s] digest = %s, want %s", tag, got, d)
			}
		}
	})

	t.Run("no-clobber skips existing tags", func(t *testing.T) {
		src := newCopyTestRegistry(t)
		dst := newCopyTestRegistry(t)
		seedCopyTag(t, src, "app", "v1")
		wantV2 := seedCopyTag(t, src, "app", "v2")
		existing := seedCopyTag(t, dst, "app", "v1")

		o := &flags.ImageCopyOpts{PlainHTTP: true, AllTags: true, NoClobber: true, Jobs: 2}
		if err := CopyImageCmd(context.Background(), o, src+"/app", dst+"/app", ro); err != nil {
			t.Fatalf("CopyImageCmd: %v", err)
		}
		if got := copyTagDigest(t, dst, "app", "v1"); got != existing {
			t.Errorf("tag [v1] digest = %s, want untouched %s", got, existing)
		}
		if got := copyTagDigest(t, dst, "app", "v2"); got != wantV2 {
			t.Errorf("tag [v2] digest = %s, want %s", got, wantV2)
		}
	})
}

func TestCopyImageCmdBadPlatformFailsFast(t *testing.T) {
	src := newCopyTestRegistry(t)
	dst := newCopyTestRegistry(t)
	seedCopyTag(t, src, "app", "v1")

	o := &flags.ImageCopyOpts{PlainHTTP: true, Platform: "linux/amd64/v8/extra"}
	start := time.Now()
	err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:v1", &flags.CliRootOpts{AuditLevel: "none"})
	if err == nil || !strings.Contains(err.Error(), "failed to parse platform") {
		t.Fatalf("expected a platform parse error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("bad --platform took %s... it should fail before any retry", elapsed)
	}
}

func TestCopyCmdJobsValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--jobs", "2"}, "--jobs requires --all-tags"},
		{[]string{"--all-tags", "--jobs", "0"}, "invalid --jobs [0]"},
	} {
		root := &cobra.Command{Use: "hauler"}
		addCopy(root, &flags.CliRootOpts{AuditLevel: "none"})
		root.SetArgs(append([]string{"copy", "src/a", "dst/a"}, tc.args...))
		root.SilenceUsage = true
		root.SilenceErrors = true
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %v: expected error containing [%s], got %v", tc.args, tc.want, err)
		}
	}
}

// newExtrasTestRegistry starts an in-memory registry with or without the referrers api and returns its host:port
func newExtrasTestRegistry(t *testing.T, referrersAPI bool) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.WithReferrersSupport(referrersAPI)))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// seedSignedIndex pushes a multi-arch index to host/repo:tag with a cosign .sig tag on the index and an oci referrer on its first child
func seedSignedIndex(t *testing.T, host, repo, tag string) (sigTag string, sigDigest, referrerDigest, child gv1.Hash) {
	t.Helper()
	idx, err := random.Index(256, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	idxRef, _ := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err := remote.WriteIndex(idxRef, idx); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	idxDigest, _ := idx.Digest()

	sig, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	sigTag = strings.ReplaceAll(idxDigest.String(), ":", "-") + ".sig"
	sigRef, _ := name.NewTag(host+"/"+repo+":"+sigTag, name.Insecure)
	if err := remote.Write(sigRef, sig); err != nil {
		t.Fatalf("seed sig: %v", err)
	}
	sigDigest, _ = sig.Digest()

	m, err := idx.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	childDesc := m.Manifests[0]
	bundle := mutate.ConfigMediaType(mutate.MediaType(empty.Image, gvtypes.OCIManifestSchema1), "application/vnd.dev.sigstore.bundle.v0.3+json")
	bundle = mutate.Subject(bundle, childDesc).(gv1.Image)
	referrerDigest, _ = bundle.Digest()
	bundleRef, _ := name.NewDigest(host+"/"+repo+"@"+referrerDigest.String(), name.Insecure)
	if err := remote.Write(bundleRef, bundle); err != nil {
		t.Fatalf("seed referrer: %v", err)
	}
	return sigTag, sigDigest, referrerDigest, childDesc.Digest
}

// copyReferrers returns the referrer digests the destination reports for host/repo@subject
func copyReferrers(t *testing.T, host, repo string, subject gv1.Hash) []string {
	t.Helper()
	ref, _ := name.NewDigest(host+"/"+repo+"@"+subject.String(), name.Insecure)
	idx, err := remote.Referrers(ref)
	if err != nil {
		t.Fatalf("referrers for [%s]: %v", ref, err)
	}
	m, err := idx.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range m.Manifests {
		out = append(out, d.Digest.String())
	}
	return out
}

func TestCopyImageCmdExtras(t *testing.T) {
	ro := &flags.CliRootOpts{AuditLevel: "none"}
	for _, referrersAPI := range []bool{false, true} {
		for _, allTags := range []bool{false, true} {
			src := newExtrasTestRegistry(t, referrersAPI)
			sigTag, sigDigest, referrerDigest, child := seedSignedIndex(t, src, "app", "v1")
			srcArg := src + "/app:v1"
			if allTags {
				srcArg = src + "/app"
			}
			dstArg := func(dst string) string {
				if allTags {
					return dst + "/app"
				}
				return dst + "/app:v1"
			}

			mode := fmt.Sprintf("referrers api %t all tags %t", referrersAPI, allTags)

			t.Run(mode+" copies extras", func(t *testing.T) {
				dst := newExtrasTestRegistry(t, referrersAPI)
				o := &flags.ImageCopyOpts{PlainHTTP: true, AllTags: allTags, Jobs: 2}
				if err := CopyImageCmd(context.Background(), o, srcArg, dstArg(dst), ro); err != nil {
					t.Fatalf("CopyImageCmd (referrersAPI=%t allTags=%t): %v", referrersAPI, allTags, err)
				}
				if got := copyTagDigest(t, dst, "app", sigTag); got != sigDigest.String() {
					t.Errorf("referrersAPI=%t allTags=%t: sig tag digest = %q, want %s", referrersAPI, allTags, got, sigDigest)
				}
				if got := copyReferrers(t, dst, "app", child); len(got) != 1 || got[0] != referrerDigest.String() {
					t.Errorf("referrersAPI=%t allTags=%t: child referrers = %v, want [%s]", referrersAPI, allTags, got, referrerDigest)
				}
			})

			t.Run(mode+" exclude-extras skips them", func(t *testing.T) {
				dst := newExtrasTestRegistry(t, referrersAPI)
				o := &flags.ImageCopyOpts{PlainHTTP: true, AllTags: allTags, Jobs: 2, ExcludeExtras: true}
				if err := CopyImageCmd(context.Background(), o, srcArg, dstArg(dst), ro); err != nil {
					t.Fatalf("CopyImageCmd (referrersAPI=%t allTags=%t): %v", referrersAPI, allTags, err)
				}
				dstRepo, _ := name.NewRepository(dst+"/app", name.Insecure)
				tags, err := remote.List(dstRepo)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Join(tags, ",") != "v1" {
					t.Errorf("referrersAPI=%t allTags=%t: destination tags = %v, want [v1]", referrersAPI, allTags, tags)
				}
				if got := copyReferrers(t, dst, "app", child); len(got) != 0 {
					t.Errorf("referrersAPI=%t allTags=%t: child referrers = %v, want none", referrersAPI, allTags, got)
				}
			})
		}
	}
}

func TestCopyImageCmdExtrasProbeErrorFails(t *testing.T) {
	reg := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			http.Error(w, "boom", http.StatusBadRequest)
			return
		}
		reg.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	src := strings.TrimPrefix(srv.URL, "http://")
	dst := newCopyTestRegistry(t)
	seedCopyTag(t, src, "app", "v1")

	o := &flags.ImageCopyOpts{PlainHTTP: true, Retries: 1}
	err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:v1", &flags.CliRootOpts{AuditLevel: "none"})
	if err == nil || !strings.Contains(err.Error(), ".sig") {
		t.Fatalf("expected the .sig probe error to fail the copy, got %v", err)
	}

	o.ExcludeExtras = true
	if err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:v1", &flags.CliRootOpts{AuditLevel: "none"}); err != nil {
		t.Fatalf("expected --exclude-extras to skip the probe, got %v", err)
	}
}

func TestCopyImageCmdPlatformExtras(t *testing.T) {
	src := newExtrasTestRegistry(t, false)
	dst := newExtrasTestRegistry(t, false)

	amd64, _ := random.Image(256, 1)
	arm64, _ := random.Image(256, 1)
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amd64, Descriptor: gv1.Descriptor{Platform: &gv1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: arm64, Descriptor: gv1.Descriptor{Platform: &gv1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	idxRef, _ := name.NewTag(src+"/app:v1", name.Insecure)
	if err := remote.WriteIndex(idxRef, idx); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	idxDigest, _ := idx.Digest()
	amd64Digest, _ := amd64.Digest()

	// a .sig on the index and one on the amd64 child
	for _, d := range []gv1.Hash{idxDigest, amd64Digest} {
		sig, _ := random.Image(64, 1)
		ref, _ := name.NewTag(src+"/app:"+strings.ReplaceAll(d.String(), ":", "-")+".sig", name.Insecure)
		if err := remote.Write(ref, sig); err != nil {
			t.Fatalf("seed sig: %v", err)
		}
	}

	o := &flags.ImageCopyOpts{PlainHTTP: true, Platform: "linux/amd64"}
	if err := CopyImageCmd(context.Background(), o, src+"/app:v1", dst+"/app:amd64", &flags.CliRootOpts{AuditLevel: "none"}); err != nil {
		t.Fatalf("CopyImageCmd: %v", err)
	}
	if got := copyTagDigest(t, dst, "app", "amd64"); got != amd64Digest.String() {
		t.Errorf("destination digest = %s, want the amd64 child %s", got, amd64Digest)
	}
	if got := copyTagDigest(t, dst, "app", strings.ReplaceAll(amd64Digest.String(), ":", "-")+".sig"); got == "" {
		t.Error("expected the amd64 child's .sig to be copied")
	}
	if got := copyTagDigest(t, dst, "app", strings.ReplaceAll(idxDigest.String(), ":", "-")+".sig"); got != "" {
		t.Error("expected the index's .sig to be left behind with --platform")
	}
}
