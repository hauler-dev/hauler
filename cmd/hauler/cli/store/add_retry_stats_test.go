package store

// add_retry_stats_test.go covers a single narrow bug in storeImage
// (cmd/hauler/cli/store/add.go): a retried s.AddImage attempt must not
// accumulate layer count / byte totals onto the same store.ImageStats
// pointer used by a prior, failed attempt -- otherwise the "✓ added ..."
// completion line double (or N-x) counts layers/bytes across retries.

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rs/zerolog"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// failOnceForDigestHandler wraps an http.Handler and fails the very first GET
// request for one specific blob digest with a 403, then lets every other
// request -- including every later request for that same digest -- through
// unmodified. Targeting a single, specific digest (rather than "the first
// request for whichever digest shows up first") is deliberate: writeImageBlobs
// fetches an image's layers concurrently and its config sequentially
// afterward, so a naive "fail every digest's first-ever request" approach
// makes the *retry* attempt also fail (on whichever blob it reaches first
// that the earlier, aborted attempt never got around to requesting at all).
// Failing exactly one predetermined digest, exactly once, guarantees the
// first AddImage attempt fails (on that blob) while the second, retried
// attempt succeeds outright (every blob, including the previously-failing
// one, now passes through).
//
// 403 (rather than 500/502/503/504/408/429) is deliberate: go-containerregistry's
// remote transport has its own built-in retry for a fixed set of "temporary"
// status codes (see remote.defaultRetryStatusCodes) and would silently absorb
// a transient 500 within a single AddImage call (up to 3 internal attempts).
// 403 isn't in that set, so it surfaces as a hard error from that AddImage
// call and exercises this package's own retry.Operation-driven retry instead.
type failOnceForDigestHandler struct {
	next   http.Handler
	target string // e.g. "sha256:abcd..."

	mu     sync.Mutex
	failed bool
}

func (f *failOnceForDigestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/blobs/"+f.target) {
		f.mu.Lock()
		alreadyFailed := f.failed
		f.failed = true
		f.mu.Unlock()

		if !alreadyFailed {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	f.next.ServeHTTP(w, r)
}

// newFailOnceRegistry starts an in-memory OCI registry, listening on
// "localhost:0" (so go-containerregistry auto-selects plain HTTP, matching
// newLocalhostRegistry in add_test.go), wrapped in a failOnceForDigestHandler.
// The returned handler's target field must be set (to the digest that should
// fail exactly once) before the read that should fail; that happens after
// seeding, once the seeded image's layer digest is known, and before the
// caller reads the image back via AddImage/storeImage.
func newFailOnceRegistry(t *testing.T) (host string, remoteOpts []remote.Option, handler *failOnceForDigestHandler) {
	t.Helper()
	handler = &failOnceForDigestHandler{next: registry.New()}

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newFailOnceRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts, handler
}

// addedLineLayerCount extracts the layer count from the last "✓ added ...
// (N layer(s), ...)" line in out. Fails the test if no such line is found.
func addedLineLayerCount(t *testing.T, out string) int {
	t.Helper()
	re := regexp.MustCompile(`✓ added .*\((\d+) layers?,`)
	matches := re.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		t.Fatalf("no \"✓ added ... (N layer(s), ...)\" line found in output:\n%s", out)
	}
	last := matches[len(matches)-1]
	n := 0
	for _, c := range last[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// TestStoreImage_RetryDoesNotDoubleCountStats is the regression test for the
// ImageStats double-counting bug: storeImage built a single store.ImageStats
// pointer outside retry.Operation's closure, so a failed first attempt that
// had already accumulated layer/byte counts into it left those counts in
// place for a subsequent, successful retry attempt to accumulate on top of --
// inflating the final completion line's layer/byte totals.
//
// seedImage (testhelpers_test.go) always creates a 2-layer image. A single,
// uncontested successful AddImage call reports "2 layers"; the bug would
// report "4 layers" after exactly one failed attempt followed by one
// successful retry.
func TestStoreImage_RetryDoesNotDoubleCountStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	host, remoteOpts, handler := newFailOnceRegistry(t)
	seededImg := seedImage(t, host, "test/retry-stats", "v1", remoteOpts...)

	layers, err := seededImg.Layers()
	if err != nil {
		t.Fatalf("seededImg.Layers: %v", err)
	}
	if len(layers) == 0 {
		t.Fatal("seeded image has no layers")
	}
	targetDigest, err := layers[0].Digest()
	if err != nil {
		t.Fatalf("layers[0].Digest: %v", err)
	}
	handler.target = targetDigest.String()

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(context.Background())

	rso := defaultRootOpts(s.Root)
	rso.Retries = 2 // one failed attempt + one successful retry
	ro := defaultCliOpts()

	cfg := v1.Image{Name: host + "/test/retry-stats:v1"}
	if err := storeImage(ctx, s, cfg, "", true /* excludeExtras: keep this to just the image's own layers */, rso, ro, ""); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	out := buf.String()
	if got := addedLineLayerCount(t, out); got != 2 {
		t.Errorf("reported layer count = %d, want 2 (seedImage's fixed layer count); a retried attempt must not double-count onto the prior failed attempt's stats\nfull output:\n%s", got, out)
	}
}
