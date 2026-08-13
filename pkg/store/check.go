package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
)

// BlobStatus describes the outcome of checking a single blob against its descriptor.
type BlobStatus string

const (
	BlobOK             BlobStatus = "ok"
	BlobMissing        BlobStatus = "missing"
	BlobSizeMismatch   BlobStatus = "size-mismatch"
	BlobDigestMismatch BlobStatus = "digest-mismatch"
	BlobUnreadable     BlobStatus = "unreadable"
)

// BlobResult is the outcome for one blob. Detail is human-readable context
// (e.g. "expected 4194304 bytes, found 1050624").
type BlobResult struct {
	Digest string
	Status BlobStatus
	Detail string
}

// CheckResult aggregates a descriptor graph. Problems is empty when OK.
type CheckResult struct {
	OK       bool
	Problems []BlobResult
}

// checkEntry memoizes the check result for a single digest so that a blob
// shared across many artifacts (e.g. a common base layer) is only hashed once per
// Checker, no matter how many goroutines request it concurrently: sync.Once blocks
// concurrent callers until the first caller's CheckBlob call has completed.
type checkEntry struct {
	once   sync.Once
	result BlobResult
}

// Checker recomputes and checks the on-disk content of blobs referenced by a
// Layout's index, detecting blobs left truncated or corrupted by an interrupted
// pull. A Checker is intended for the lifetime of a single command invocation:
// it memoizes results per-digest so repeated references to the same blob (e.g. a
// shared base layer across multiple images) are only hashed once.
type Checker struct {
	l *Layout

	mu   sync.Mutex
	memo map[string]*checkEntry

	// hashMu/hashCounts track, per-digest, how many times CheckBlob actually
	// streamed a blob's content through a digest verifier (as opposed to
	// short-circuiting on a missing-file or size-mismatch check). This has no
	// effect on check results; it exists so tests can prove that
	// size-mismatch short-circuiting and per-digest memoization avoid redundant
	// hashing of large blobs.
	hashMu     sync.Mutex
	hashCounts map[string]int
}

// NewChecker returns a Checker bound to l.
func (l *Layout) NewChecker() *Checker {
	return &Checker{
		l:          l,
		memo:       make(map[string]*checkEntry),
		hashCounts: make(map[string]int),
	}
}

// HashCount returns the number of times the blob identified by digestStr (e.g.
// "sha256:abc...") was actually streamed through a digest verifier.
func (c *Checker) HashCount(digestStr string) int {
	c.hashMu.Lock()
	defer c.hashMu.Unlock()
	return c.hashCounts[digestStr]
}

func (c *Checker) recordHash(digestStr string) {
	c.hashMu.Lock()
	c.hashCounts[digestStr]++
	c.hashMu.Unlock()
}

// CheckBlob checks exactly one blob on disk against its descriptor. It does not
// decode the blob or recurse into anything it references.
//
// Checks run in this order, each short-circuiting the next:
//  1. The blob file must exist at its content-addressed path under
//     <root>/blobs/<algorithm>/<hex>.
//  2. If desc.Size is known (> 0), the file size on disk must match exactly --
//     a mismatch here is reported without ever reading the file's content.
//  3. The file's content is streamed (never fully buffered in memory) through
//     the descriptor's digest verifier.
func (c *Checker) CheckBlob(desc ocispec.Descriptor) BlobResult {
	digestStr := desc.Digest.String()

	blobFile := filepath.Join(c.l.Root, ocispec.ImageBlobsDir, desc.Digest.Algorithm().String(), desc.Digest.Hex())

	info, err := os.Stat(blobFile)
	if err != nil {
		if os.IsNotExist(err) {
			return BlobResult{
				Digest: digestStr,
				Status: BlobMissing,
				Detail: fmt.Sprintf("blob not found at %s", blobFile),
			}
		}
		return BlobResult{
			Digest: digestStr,
			Status: BlobUnreadable,
			Detail: err.Error(),
		}
	}

	if desc.Size > 0 && info.Size() != desc.Size {
		return BlobResult{
			Digest: digestStr,
			Status: BlobSizeMismatch,
			Detail: fmt.Sprintf("expected %d bytes, found %d", desc.Size, info.Size()),
		}
	}

	if !desc.Digest.Algorithm().Available() {
		return BlobResult{
			Digest: digestStr,
			Status: BlobUnreadable,
			Detail: fmt.Sprintf("digest algorithm %q is not available in this build", desc.Digest.Algorithm()),
		}
	}

	f, err := os.Open(blobFile)
	if err != nil {
		return BlobResult{
			Digest: digestStr,
			Status: BlobUnreadable,
			Detail: err.Error(),
		}
	}
	defer f.Close()

	// dv (go-digest's Verifier) is the hash-comparison API from
	// github.com/opencontainers/go-digest, not this package's Checker type.
	dv := desc.Digest.Verifier()
	c.recordHash(digestStr)
	if _, err := io.Copy(dv, f); err != nil {
		return BlobResult{
			Digest: digestStr,
			Status: BlobUnreadable,
			Detail: fmt.Sprintf("reading blob content: %v", err),
		}
	}

	if !dv.Verified() {
		return BlobResult{
			Digest: digestStr,
			Status: BlobDigestMismatch,
			Detail: "content does not match its digest",
		}
	}

	return BlobResult{Digest: digestStr, Status: BlobOK}
}

// checkMemo returns the memoized CheckBlob result for desc.Digest, computing it
// at most once per Checker lifetime even when called concurrently for the same
// digest from multiple goroutines.
func (c *Checker) checkMemo(desc ocispec.Descriptor) BlobResult {
	key := desc.Digest.String()

	c.mu.Lock()
	entry, ok := c.memo[key]
	if !ok {
		entry = &checkEntry{}
		c.memo[key] = entry
	}
	c.mu.Unlock()

	entry.once.Do(func() {
		entry.result = c.CheckBlob(desc)
	})
	return entry.result
}

// manifestLike captures the fields common to both an OCI image manifest and an OCI
// image index, letting Check walk either shape uniformly -- a sibling of the
// anonymous decode struct used by Layout.CleanUp, but performing a real byte
// check instead of only marking digests as referenced.
type manifestLike struct {
	Config    ocispec.Descriptor   `json:"config"`
	Layers    []ocispec.Descriptor `json:"layers"`
	Manifests []ocispec.Descriptor `json:"manifests"`
}

// Check walks the descriptor graph rooted at desc -- the manifest blob itself,
// its config and layers, and (for an image index) each child manifest recursively
// -- and aggregates every blob that fails its check.
//
// If the manifest blob itself fails its check, Check stops descending: a
// manifest whose own bytes don't match its digest cannot be trusted to accurately
// name its children, so attempting to check those children would be meaningless.
// A manifest that passes its own digest check but fails to decode as JSON is
// reported as BlobUnreadable (the bytes are correct per-digest, but malformed).
func (c *Checker) Check(ctx context.Context, desc ocispec.Descriptor) CheckResult {
	result := CheckResult{OK: true}

	manifestResult := c.checkMemo(desc)
	if manifestResult.Status != BlobOK {
		result.OK = false
		result.Problems = append(result.Problems, manifestResult)
		return result
	}

	rc, err := c.l.OCI.Fetch(ctx, desc)
	if err != nil {
		result.OK = false
		result.Problems = append(result.Problems, BlobResult{
			Digest: desc.Digest.String(),
			Status: BlobUnreadable,
			Detail: fmt.Sprintf("fetching manifest: %v", err),
		})
		return result
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		result.OK = false
		result.Problems = append(result.Problems, BlobResult{
			Digest: desc.Digest.String(),
			Status: BlobUnreadable,
			Detail: fmt.Sprintf("reading manifest: %v", err),
		})
		return result
	}

	var m manifestLike
	if err := json.Unmarshal(data, &m); err != nil {
		result.OK = false
		result.Problems = append(result.Problems, BlobResult{
			Digest: desc.Digest.String(),
			Status: BlobUnreadable,
			Detail: fmt.Sprintf("decoding manifest JSON: %v", err),
		})
		return result
	}

	var (
		problemsMu sync.Mutex
		g          errgroup.Group
	)
	g.SetLimit(runtime.GOMAXPROCS(0))

	addProblem := func(r BlobResult) {
		problemsMu.Lock()
		result.Problems = append(result.Problems, r)
		problemsMu.Unlock()
	}

	if m.Config.Digest.Validate() == nil {
		configDesc := m.Config
		g.Go(func() error {
			if r := c.checkMemo(configDesc); r.Status != BlobOK {
				addProblem(r)
			}
			return nil
		})
	}

	for _, l := range m.Layers {
		if l.Digest.Validate() != nil {
			continue
		}
		lyr := l
		g.Go(func() error {
			if r := c.checkMemo(lyr); r.Status != BlobOK {
				addProblem(r)
			}
			return nil
		})
	}

	// The scheduled functions above never return a non-nil error; g.Wait() is
	// used purely to bound concurrency and wait for completion.
	_ = g.Wait()

	for _, child := range m.Manifests {
		if child.Digest.Validate() != nil {
			continue
		}
		childResult := c.Check(ctx, child)
		if !childResult.OK {
			result.Problems = append(result.Problems, childResult.Problems...)
		}
	}

	if len(result.Problems) > 0 {
		result.OK = false
	}

	return result
}
