package cli

// store_info_check_test.go is a regression guard for `hauler store info --check`
// exercised through the real cobra command wiring (addStoreInfo's PreRunE + RunE),
// not by calling store.InfoCmd directly. The existing coverage in
// cmd/hauler/cli/store/info_test.go calls InfoCmd directly and so never exercises
// addStoreInfo's PreRunE, which is exactly why the Fix 1/2 bugs (corruption summary
// silently swallowed by the json-output log suppression, and that suppression firing
// even when --check was never requested) went undetected until manual e2e testing.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// newInfoCheckTestStore creates a fresh store in a temp directory along with an
// in-memory OCI registry (backed by httptest) that the store can pull from.
func newInfoCheckTestStore(t *testing.T) (s *store.Layout, host string) {
	t.Helper()

	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("store.NewLayout: %v", err)
	}

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")

	return s, host
}

// seedInfoCheckImage pushes a random single-platform image to host/repo:tag.
func seedInfoCheckImage(t *testing.T, host, repo, tag string) {
	t.Helper()

	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}
}

// findInfoCheckManifestDescriptor walks s and returns the descriptor for the first
// entry whose AnnotationRefName contains refSubstring. Fails the test if none found.
func findInfoCheckManifestDescriptor(t *testing.T, s *store.Layout, refSubstring string) ocispec.Descriptor {
	t.Helper()

	var found ocispec.Descriptor
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) {
			found = desc
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found.Digest == "" {
		t.Fatalf("no artifact found for ref containing %q", refSubstring)
	}
	return found
}

// corruptInfoCheckBlob flips the first byte of the blob identified by d, preserving
// its exact length -- a same-length corruption that only a real digest check catches.
func corruptInfoCheckBlob(t *testing.T, root string, d digest.Digest) {
	t.Helper()

	path := filepath.Join(root, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read blob %s: %v", d, err)
	}
	corrupted := append([]byte(nil), data...)
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatalf("corrupt blob %s: %v", d, err)
	}
}

// captureStdoutAndStderr redirects os.Stdout and os.Stderr for the duration of fn,
// returning everything written to each separately along with fn's return value.
//
// fn must perform any log.NewLogger call *inside* itself (after the swap), since
// pkg/log's zerolog.ConsoleWriter captures the concrete *os.File value of os.Stdout
// at construction time, not a live reference to the os.Stdout variable.
func captureStdoutAndStderr(t *testing.T, fn func() error) (stdout, stderr string, fnErr error) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout): %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stderr): %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW

	fnErr = fn()

	outW.Close()
	errW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	if _, err := io.Copy(&outBuf, outR); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if _, err := io.Copy(&errBuf, errR); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	outR.Close()
	errR.Close()

	return outBuf.String(), errBuf.String(), fnErr
}

// TestStoreInfoCheck_JSON_CorruptArtifactInPayload exercises `hauler store info
// --check -o json` through the real cobra wiring. Under the current design, the
// corruption summary/remediation lines go through normal log.FromContext calls
// unconditionally -- no more stderr special-casing -- and are expected to be
// silently suppressed by the existing PreRunE's `--check && -o json` ->
// SetLevel("fatal") gating. The corrupt artifact's presence in the JSON body
// (with a non-empty problems array) is the machine-readable signal instead.
func TestStoreInfoCheck_JSON_CorruptArtifactInPayload(t *testing.T) {
	s, host := newInfoCheckTestStore(t)
	ctx := context.Background()

	seedInfoCheckImage(t, host, "test/corrupt", "v1")
	if _, err := s.AddImage(ctx, host+"/test/corrupt:v1", "", true, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	desc := findInfoCheckManifestDescriptor(t, s, "test/corrupt")
	corruptInfoCheckBlob(t, s.Root, desc.Digest)

	rso := &flags.StoreRootOpts{StoreDir: s.Root, Retries: 1}
	ro := &flags.CliRootOpts{LogLevel: "error", AuditLevel: "none", HaulerDir: t.TempDir()}

	cmd := addStoreInfo(rso, ro)
	cmd.SetArgs([]string{"--check", "-o", "json"})

	stdout, stderr, err := captureStdoutAndStderr(t, func() error {
		logger := log.NewLogger(os.Stdout)
		execCtx := logger.WithContext(ctx)
		return cmd.ExecuteContext(execCtx)
	})

	if err != nil {
		t.Fatalf("execute: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	var out struct {
		Artifacts []struct {
			Reference string   `json:"reference"`
			Problems  []string `json:"problems"`
		} `json:"artifacts"`
	}
	if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q\nstderr: %q", jerr, stdout, stderr)
	}

	if len(out.Artifacts) != 1 {
		t.Fatalf("expected exactly one artifact in the failure report, got %d: %+v", len(out.Artifacts), out.Artifacts)
	}
	if len(out.Artifacts[0].Problems) == 0 {
		t.Errorf("expected non-empty problems for the corrupt artifact, got: %+v", out.Artifacts[0])
	}
}

// TestStoreInfoCheck_JSON_NoCheckStillWorks exercises `hauler store info -o json`
// (no --check) through the real cobra wiring. It is the regression guard for Fix 2:
// narrowing the json-output log suppression to `--check && -o json` must not break
// (or leave globally suppressed) the plain `-o json` path, which never emitted a
// corruption-summary log in the first place.
func TestStoreInfoCheck_JSON_NoCheckStillWorks(t *testing.T) {
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	s, host := newInfoCheckTestStore(t)
	ctx := context.Background()

	seedInfoCheckImage(t, host, "test/plain", "v1")
	if _, err := s.AddImage(ctx, host+"/test/plain:v1", "", true, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	rso := &flags.StoreRootOpts{StoreDir: s.Root, Retries: 1}
	ro := &flags.CliRootOpts{LogLevel: "error", AuditLevel: "none", HaulerDir: t.TempDir()}

	cmd := addStoreInfo(rso, ro)
	cmd.SetArgs([]string{"-o", "json"})

	stdout, stderr, err := captureStdoutAndStderr(t, func() error {
		logger := log.NewLogger(os.Stdout)
		execCtx := logger.WithContext(ctx)
		return cmd.ExecuteContext(execCtx)
	})

	if err != nil {
		t.Fatalf("execute: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	var out map[string]interface{}
	if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q\nstderr: %q", jerr, stdout, stderr)
	}

	if zerolog.GlobalLevel() == zerolog.FatalLevel {
		t.Error("global log level was clamped to fatal even though --check was not requested")
	}
}
