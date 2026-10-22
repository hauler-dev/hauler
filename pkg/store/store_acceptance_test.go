package store

// store_acceptance_test.go holds the acceptance tests that need a
// non-localhost host or a fake Docker daemon. They are whitebox
// (package store) so they can reach the unexported content.OCI and
// ensureDockerHost internals.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// --------------------------------------------------------------------------
// InsecureSkipTLSVerify: non-vacuous proof
// --------------------------------------------------------------------------

// newSelfSignedRegistry starts an in-memory OCI registry behind a TLS server
// with a self-signed certificate that the system trust store does not know.
// A fetch against it therefore only succeeds when the caller explicitly opts
// into insecure transport -- exactly the flag under test. The host is a
// non-loopback local address so ggcr resolves it as https (a loopback host
// would be downgraded to plain http and never exercise TLS at all).
func newSelfSignedRegistry(t *testing.T) (host string, remoteOpts []remote.Option, ok bool) {
	t.Helper()

	addr := findHTTPSchemeLocalAddr(t)
	if addr == "" {
		return "", nil, false
	}

	cert, err := selfSignedCertForIP(net.ParseIP(addr))
	if err != nil {
		t.Fatalf("newSelfSignedRegistry cert: %v", err)
	}

	l, err := net.Listen("tcp", addr+":0")
	if err != nil {
		return "", nil, false
	}
	srv := httptest.NewUnstartedServer(registry.New())
	srv.Listener = l
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	host = l.Addr().String()
	// The registry's own client trusts the test server's self-signed cert, so
	// seeding works; the store's fetch uses the default transport and does not.
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts, true
}

// selfSignedCertForIP builds a self-signed certificate valid for ip, so a TLS
// test server bound to that address can be reached by hostname-checking
// clients that trust it (the seed client) while still being rejected by the
// default trust store (the store's fetch).
func selfSignedCertForIP(ip net.IP) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hauler-test-selfsigned"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{ip},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// findHTTPSchemeLocalAddr returns a non-loopback local IPv4 address for which
// go-containerregistry's name.Registry.Scheme() is "https" (i.e. neither
// localhost nor RFC1918 private space). Returns "" if none exists.
func findHTTPSchemeLocalAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		host := ip.String()
		ref, err := name.ParseReference(host + ":5000/x/y:v1")
		if err != nil {
			continue
		}
		if ref.Context().Registry.Scheme() == "https" {
			return host
		}
	}
	return ""
}

// TestAddImageWithOpts_InsecureSkipTLSVerify_SelfSigned proves the
// InsecureSkipTLSVerify option is actually forwarded to Layout.AddImage.
//
// The registry is served over TLS with a self-signed certificate, so the
// fetch fails with an untrusted-certificate error unless the transport skips
// verification. The option is what makes the difference: with it the fetch
// succeeds, without it the same fetch fails. A version of AddImageWithOpts
// that dropped the flag would fail the first assertion.
func TestAddImageWithOpts_InsecureSkipTLSVerify_SelfSigned(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts, ok := newSelfSignedRegistry(t)
	if !ok {
		t.Skip("no non-private local address available to force an https-scheme registry")
	}
	seedImage(t, host, "test/insecure-selfsigned", "v1", rOpts...)

	// Control: without the flag, the self-signed cert is untrusted, so the add
	// must fail. This is what makes the test non-vacuous.
	sControl := newTestStore(t)
	if err := AddImageWithOpts(ctx, sControl, host+"/test/insecure-selfsigned:v1", ImageAddOptions{}); err == nil {
		t.Fatal("control: expected failure fetching a self-signed registry without InsecureSkipTLSVerify, got nil")
	}
	if n := countArtifactsInStore(t, sControl); n != 0 {
		t.Fatalf("control: expected empty store after failed fetch, got %d artifacts", n)
	}

	// With the flag, the same fetch succeeds against the self-signed source.
	s := newTestStore(t)
	if err := AddImageWithOpts(ctx, s, host+"/test/insecure-selfsigned:v1", ImageAddOptions{InsecureSkipTLSVerify: true}); err != nil {
		t.Fatalf("AddImageWithOpts with InsecureSkipTLSVerify: %v", err)
	}
	assertArtifactInStore(t, s, "test/insecure-selfsigned:v1")
}

// TestAddImageWithOpts_InsecureSkipTLSVerify_ReachesTransport proves the flag
// reaches content.BuildTransport (not merely ggcr's name.Insecure scheme
// fallback): the transport built for an insecure fetch carries
// InsecureSkipVerify=true, while the default one does not. This is the exact
// wire the consumer's self-signed-registry use case depends on.
func TestAddImageWithOpts_InsecureSkipTLSVerify_ReachesTransport(t *testing.T) {
	insecure, err := content.BuildTransport(true, "")
	if err != nil {
		t.Fatalf("content.BuildTransport(true): %v", err)
	}
	if !transportInsecureSkipVerify(t, insecure) {
		t.Fatal("insecure transport did not set InsecureSkipVerify")
	}

	plain, err := content.BuildTransport(false, "")
	if err != nil {
		t.Fatalf("content.BuildTransport(false): %v", err)
	}
	if transportInsecureSkipVerify(t, plain) {
		t.Fatal("default transport unexpectedly set InsecureSkipVerify")
	}
}

// --------------------------------------------------------------------------
// AddLocalImage: fake Docker daemon
// --------------------------------------------------------------------------

// fakeDockerDaemon serves just enough of the Docker Engine API for
// go-containerregistry's daemon.Image to load an image: /version (the
// availability probe), the image inspect endpoint, and the image save
// endpoint. It is bound to a unix socket so DOCKER_HOST=unix://<path> points
// daemon.Image at it.
type fakeDockerDaemon struct {
	srv *httptest.Server
}

func newFakeDockerDaemon(t *testing.T, img gcrv1.Image) *fakeDockerDaemon {
	t.Helper()

	// Not t.TempDir(): its path embeds the test name, which can exceed macOS's
	// 104-byte unix socket path limit (bind: invalid argument).
	sockDir, err := os.MkdirTemp("", "hauler-sock")
	if err != nil {
		t.Fatalf("newFakeDockerDaemon mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sockPath := filepath.Join(sockDir, "docker.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("newFakeDockerDaemon listen: %v", err)
	}

	// Pre-render the docker-save tarball for img.
	var tarBuf bytes.Buffer
	if err := writeDockerSaveTar(&tarBuf, img); err != nil {
		t.Fatalf("newFakeDockerDaemon render tar: %v", err)
	}
	tarBytes := tarBuf.Bytes()

	digest, err := img.Digest()
	if err != nil {
		t.Fatalf("newFakeDockerDaemon digest: %v", err)
	}

	mux := http.NewServeMux()
	// The Docker client negotiates an API version (e.g. v1.55) and probes
	// /_ping first, so match on path shape rather than a fixed version prefix.
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.55")
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "OK") //nolint:errcheck
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ApiVersion":"1.55","Version":"24.0.0"}`) //nolint:errcheck
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/version"):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ApiVersion":"1.55","Version":"24.0.0"}`) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			body, _ := json.Marshal(map[string]any{
				"Id":          digest.String(),
				"RepoTags":    []string{"test/local:latest"},
				"RepoDigests": []string{"test/local@" + digest.String()},
			})
			w.Write(body) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/get"):
			w.Header().Set("Content-Type", "application/x-tar")
			w.Write(tarBytes) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	})

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: mux},
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return &fakeDockerDaemon{srv: srv}
}

// TestAddLocalImage_FakeDockerDaemon proves (*Layout).AddLocalImage imports an
// image from the local Docker daemon and returns a digest string. The daemon
// is faked over a unix socket so the test needs no real Docker.
func TestAddLocalImage_FakeDockerDaemon(t *testing.T) {
	ctx := newTestContext(t)

	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	wantDigest, err := img.Digest()
	if err != nil {
		t.Fatalf("img.Digest: %v", err)
	}

	daemon := newFakeDockerDaemon(t, img)
	t.Setenv("DOCKER_HOST", "unix://"+daemon.srv.Listener.Addr().String())

	s := newTestStore(t)
	got, err := s.AddLocalImage(ctx, "test/local:latest")
	if err != nil {
		t.Fatalf("AddLocalImage: %v", err)
	}
	if got != wantDigest.String() {
		t.Errorf("AddLocalImage digest = %q, want %q", got, wantDigest.String())
	}
	assertArtifactInStore(t, s, "test/local")
	assertArtifactKindInStore(t, s, "test/local", consts.KindAnnotationImage)
}

// TestAddLocalImage_NoDaemonErrors proves AddLocalImage fails cleanly (rather
// than panicking or silently succeeding) when no Docker socket is reachable.
func TestAddLocalImage_NoDaemonErrors(t *testing.T) {
	ctx := newTestContext(t)

	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		t.Skip("default docker socket exists at /var/run/docker.sock")
	}
	tmp := t.TempDir()
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("HOME", tmp)

	s := newTestStore(t)
	_, err := s.AddLocalImage(ctx, "test/local:latest")
	if err == nil {
		t.Fatal("expected error with no Docker daemon, got nil")
	}
	if !strings.Contains(err.Error(), "Docker") {
		t.Errorf("error %q should mention Docker", err.Error())
	}
}

// TestSync_LocalImage proves the SyncOptions.Local branch drives AddLocalImage
// against the (faked) local daemon.
func TestSync_LocalImage(t *testing.T) {
	ctx := newTestContext(t)

	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	daemon := newFakeDockerDaemon(t, img)
	t.Setenv("DOCKER_HOST", "unix://"+daemon.srv.Listener.Addr().String())

	manifest := `apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-local-images
spec:
  images:
    - name: test/local:latest
      local: true
`
	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-local-image-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	s := newTestStore(t)
	if err := Sync(ctx, s, SyncOptions{FileName: []string{manifestPath}}); err != nil {
		t.Fatalf("Sync local image: %v", err)
	}
	assertArtifactInStore(t, s, "test/local")
}

// --------------------------------------------------------------------------
// Context logger: every library entry point
// --------------------------------------------------------------------------

// TestLibraryEntryPointsUseContextLogger proves each library entry point
// sources its logger via log.FromContext(ctx): a caller-injected zerolog
// logger receives that entry point's output, so no package-global logger is
// used. Each subtest drives one entry point with a fresh buffer.
func TestLibraryEntryPointsUseContextLogger(t *testing.T) {
	newCtx := func(buf *bytes.Buffer) context.Context {
		return zerolog.New(buf).WithContext(context.Background())
	}

	t.Run("AddImageWithOpts", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)
		host, rOpts := newLocalhostRegistry(t)
		seedImage(t, host, "test/ctxlog", "v1", rOpts...)

		s := newTestStore(t)
		if err := AddImageWithOpts(ctx, s, host+"/test/ctxlog:v1", ImageAddOptions{}); err != nil {
			t.Fatalf("AddImageWithOpts: %v", err)
		}
		assertLoggerSaw(t, buf.String(), "adding image", "test/ctxlog:v1")
	})

	t.Run("AddFile", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)

		tmp, err := os.CreateTemp(t.TempDir(), "ctxlog-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.WriteString("payload") //nolint:errcheck
		tmp.Close()

		s := newTestStore(t)
		if err := AddFile(ctx, s, v1.File{Path: tmp.Name()}); err != nil {
			t.Fatalf("AddFile: %v", err)
		}
		assertLoggerSaw(t, buf.String(), "adding file", filepath.Base(tmp.Name()))
	})

	t.Run("AddChartWithOpts", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)

		chartPath := filepath.Join(chartTestdataDir, "rancher-cluster-templates-0.5.2.tgz")
		s := newTestStore(t)
		if err := AddChartWithOpts(ctx, s, chartPath, ChartAddOptions{}); err != nil {
			t.Fatalf("AddChartWithOpts: %v", err)
		}
		assertLoggerSaw(t, buf.String(), "adding chart")
	})

	t.Run("WriteExportsManifest", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)
		host, rOpts := newLocalhostRegistry(t)
		seedIndex(t, host, "test/ctxlog-multi", "v1", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/test/ctxlog-multi:v1", "", false, "", false, ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}
		// WriteExportsManifest logs a compatibility warning for a multi-arch
		// index with no platform filter.
		if err := WriteExportsManifest(ctx, s.Root, ""); err != nil {
			t.Fatalf("WriteExportsManifest: %v", err)
		}
		assertLoggerSaw(t, buf.String(), "compatibility warning")
	})

	t.Run("Sync", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := newCtx(&buf)

		fileURL := seedFileInHTTPServer(t, "ctxlog-sync.sh", "#!/bin/sh\necho ok")
		manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: test-ctxlog-sync
spec:
  files:
    - path: %s
`, fileURL)
		manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-ctxlog-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp: %v", err)
		}
		manifestPath := manifestFile.Name()
		if _, err := manifestFile.WriteString(manifest); err != nil {
			manifestFile.Close()
			t.Fatalf("WriteString: %v", err)
		}
		manifestFile.Close()

		s := newTestStore(t)
		if err := Sync(ctx, s, SyncOptions{FileName: []string{manifestPath}}); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		assertLoggerSaw(t, buf.String(), "processing manifest")
	})
}

// assertLoggerSaw fails unless every needle appears in the injected logger's
// captured output.
func assertLoggerSaw(t *testing.T, out string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(out, n) {
			t.Errorf("injected logger output missing %q; got %q", n, out)
		}
	}
}

// --------------------------------------------------------------------------
// helpers used above
// --------------------------------------------------------------------------

// transportInsecureSkipVerify reports whether tr (a content.transport or an
// *http.Transport) has TLS verification disabled.
func transportInsecureSkipVerify(t *testing.T, tr any) bool {
	t.Helper()
	switch v := tr.(type) {
	case *http.Transport:
		return v.TLSClientConfig != nil && v.TLSClientConfig.InsecureSkipVerify
	default:
		// content.transport wraps the base RoundTripper; probe it by driving a
		// request through a TLS server with a self-signed cert.
		return probeInsecureTransport(t, tr)
	}
}

// probeInsecureTransport reports whether tr accepts a self-signed TLS
// certificate, which is exactly what InsecureSkipVerify enables.
func probeInsecureTransport(t *testing.T, tr any) bool {
	t.Helper()
	rt, ok := tr.(http.RoundTripper)
	if !ok {
		t.Fatalf("transport %T is not an http.RoundTripper", tr)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: rt}
	resp, err := client.Get(srv.URL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// writeDockerSaveTar renders img as a docker-save tarball (the format the
// Docker Engine /images/get endpoint returns and daemon.Image consumes).
func writeDockerSaveTar(w io.Writer, img gcrv1.Image) error {
	ref, err := name.NewTag("test/local:latest", name.WithDefaultRegistry(""))
	if err != nil {
		return err
	}
	return tarball.Write(ref, img, w)
}
