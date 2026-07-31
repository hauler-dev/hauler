package content

// oci_concurrency_test.go covers thread-safety of OCI's index mutation path:
// concurrent AddIndex/Walk/SaveIndex/LoadIndex, atomicity of the on-disk
// index.json write, the UpdateAnnotations replacement for in-place Walk
// callback mutation, and the specific self-referential Walk-callback shape
// used by store.CopyAll's self-sync (sync.go:468 calls
// s.CopyAll(ctx, s.OCI, nil), which walks s.OCI while pushing back into the
// same s.OCI from inside the Walk callback).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// digestForIndex returns a distinct, syntactically valid sha256 digest for
// index i, so concurrent test goroutines never collide on digest.
func digestForIndex(i int) string {
	return fmt.Sprintf("sha256:%064x", i)
}

// newTestOCI constructs an OCI against dir and loads its index, mirroring
// how store.NewLayout always calls LoadIndex once at construction time.
// AddIndex (both before and after this task's changes) relies on o.index
// already being non-nil -- it does not call LoadIndex itself -- so any test
// that calls AddIndex directly against a bare OCI (bypassing store.Layout)
// must load the index first, exactly like this helper does.
func newTestOCI(t *testing.T, dir string) *OCI {
	t.Helper()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	if err := o.LoadIndex(); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	return o
}

// refDescriptor builds a minimal, valid-for-AddIndex descriptor for index i:
// a distinct tagged reference and a distinct digest.
func refDescriptor(i int) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(digestForIndex(i)),
		Size:      int64(100 + i),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: fmt.Sprintf("example.com/repo%d:tag%d", i, i),
			consts.KindAnnotationName: consts.KindAnnotationImage,
		},
	}
}

// --------------------------------------------------------------------------
// TestOCI_ConcurrentAddIndex
// --------------------------------------------------------------------------

// TestOCI_ConcurrentAddIndex runs many goroutines each adding a distinct
// descriptor concurrently. It must not panic/race, and -- critically -- a
// *fresh* OCI opened against the same root directory afterward must see all
// entries via LoadIndex. Checking only the original OCI's in-memory nameMap
// would not catch entries lost on disk because one goroutine's nameMap.Range
// snapshot predated another goroutine's Store.
func TestOCI_ConcurrentAddIndex(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := o.AddIndex(refDescriptor(i)); err != nil {
				errs <- fmt.Errorf("AddIndex(%d): %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Open a fresh OCI against the same root and confirm all 50 entries
	// survived to disk.
	fresh, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI (fresh): %v", err)
	}
	seen := make(map[string]bool)
	if err := fresh.Walk(func(_ string, d ocispec.Descriptor) error {
		seen[d.Annotations[ocispec.AnnotationRefName]] = true
		return nil
	}); err != nil {
		t.Fatalf("Walk (fresh): %v", err)
	}
	if len(seen) != n {
		t.Fatalf("fresh OCI sees %d entries on disk, want %d", len(seen), n)
	}
	for i := 0; i < n; i++ {
		ref := fmt.Sprintf("example.com/repo%d:tag%d", i, i)
		if !seen[ref] {
			t.Errorf("entry %q missing from disk after concurrent AddIndex", ref)
		}
	}
}

// --------------------------------------------------------------------------
// TestOCI_ConcurrentAddIndexAndWalk
// --------------------------------------------------------------------------

// TestOCI_ConcurrentAddIndexAndWalk runs AddIndex and Walk concurrently.
//
// Without the locking fix in this task, Walk hands out the live descriptor
// (and its live, shared Annotations map) straight out of nameMap while
// AddIndex/SaveIndex concurrently mutate index/nameMap on another goroutine:
// this is `fatal error: concurrent map read and map write`, a hard crash of
// the whole test binary -- not a normal per-test failure. So a green run of
// this specific test (not just "go test reported no failures") is the
// signal that the fix is in place; a regression takes down the process.
func TestOCI_ConcurrentAddIndexAndWalk(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	// Seed a few entries so Walk has something to range over from the start.
	for i := 0; i < 5; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	const n = 50
	var wg sync.WaitGroup

	for i := 5; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = o.AddIndex(refDescriptor(i))
		}(i)
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = o.Walk(func(_ string, d ocispec.Descriptor) error {
				// Read the annotation the way a real Walk caller would; this is
				// the read side of the concurrent read/write hazard.
				_ = d.Annotations[ocispec.AnnotationRefName]
				_ = d.Annotations[consts.KindAnnotationName]
				return nil
			})
		}()
	}

	wg.Wait()
}

// --------------------------------------------------------------------------
// TestOCI_SaveIndexAtomic
// --------------------------------------------------------------------------

// TestOCI_SaveIndexAtomic exercises the on-disk atomicity of SaveIndex's
// write. AddIndex/SaveIndex run concurrently (serialized through the OCI's
// internal lock) while a separate reader repeatedly reads the raw index.json
// bytes directly off disk -- bypassing the OCI's lock entirely, the way a
// second hauler process (no shared in-process mutex) would. A non-atomic
// os.WriteFile would let that reader observe a truncated or partially
// written file; the temp-file+rename approach guarantees the reader only
// ever sees a complete prior version or a complete new version.
func TestOCI_SaveIndexAtomic(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	// Seed so index.json exists before the reader starts.
	if err := o.AddIndex(refDescriptor(0)); err != nil {
		t.Fatalf("seed AddIndex: %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)

	stop := make(chan struct{})
	readErrs := make(chan error, 1)
	var readCount int
	go func() {
		for {
			select {
			case <-stop:
				readErrs <- nil
				return
			default:
			}
			data, err := os.ReadFile(indexPath)
			if err != nil {
				// A concurrent rename can transiently race an Open with ENOENT
				// on some platforms; that's not the hazard under test (torn
				// content), so tolerate it and keep polling.
				continue
			}
			readCount++
			var idx ocispec.Index
			if err := json.Unmarshal(data, &idx); err != nil {
				readErrs <- fmt.Errorf("torn/invalid index.json read: %w (raw: %s)", err, string(data))
				return
			}
		}
	}()

	const n = 50
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = o.AddIndex(refDescriptor(i))
		}(i)
	}
	wg.Wait()

	close(stop)
	if err := <-readErrs; err != nil {
		t.Fatal(err)
	}
	if readCount == 0 {
		t.Skip("reader goroutine never observed the file in time; not a meaningful run")
	}
}

// --------------------------------------------------------------------------
// TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock
// --------------------------------------------------------------------------

// TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock is the single most
// important test in this file. It reproduces, at the pkg/content level, the
// exact call shape of store.CopyAll's self-sync path
// (cmd/hauler/cli/store/sync.go:468 calls s.CopyAll(ctx, s.OCI, nil), and
// CopyAll's Walk callback calls l.Copy(ctx, reference, to, toRef) with
// to == l.OCI -- the same OCI instance Walk is iterating). Copy in turn
// calls Resolve, Fetcher, and Pusher on that same OCI.
//
// A naive `sync.Mutex` held across the entire Walk call would deadlock the
// very first time a callback calls back into Resolve/Pusher/LoadIndex on the
// same OCI -- which self-sync does on every normal `store sync` run, not
// just in some edge case. Walk must snapshot under the lock and release
// before invoking any callback.
//
// The whole test is wrapped in a short timeout so that a regression fails
// this test with a clear message instead of hanging `go test` (and CI)
// forever.
func TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	for i := 0; i < 3; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- o.Walk(func(key string, d ocispec.Descriptor) error {
			// Re-enter the same OCI instance from inside the Walk callback,
			// exactly as store.Layout.Copy does during self-sync.
			if _, err := o.Resolve(context.Background(), key); err != nil {
				return fmt.Errorf("Resolve reentrant call: %w", err)
			}
			if _, err := o.Fetcher(context.Background(), key); err != nil {
				return fmt.Errorf("Fetcher reentrant call: %w", err)
			}
			if _, err := o.Pusher(context.Background(), key); err != nil {
				return fmt.Errorf("Pusher reentrant call: %w", err)
			}
			if err := o.LoadIndex(); err != nil {
				return fmt.Errorf("LoadIndex reentrant call: %w", err)
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Walk with reentrant callback returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Walk with a callback re-entering Resolve/Fetcher/Pusher/LoadIndex on the same OCI deadlocked (timed out after 5s) -- this is the exact shape of store.CopyAll's self-sync path")
	}
}

// --------------------------------------------------------------------------
// TestOCI_UpdateAnnotations
// --------------------------------------------------------------------------

// TestOCI_UpdateAnnotations covers match+apply semantics: descriptors
// matching the predicate get their annotations replaced via apply, the
// number of matches is returned, and non-matching descriptors are untouched.
func TestOCI_UpdateAnnotations(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	for i := 0; i < 3; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	target := "example.com/repo1:tag1"
	matched, err := o.UpdateAnnotations(
		func(d ocispec.Descriptor) bool {
			return d.Annotations[ocispec.AnnotationRefName] == target
		},
		func(a map[string]string) {
			a[ocispec.AnnotationRefName] = "example.com/repo1:renamed"
		},
	)
	if err != nil {
		t.Fatalf("UpdateAnnotations: %v", err)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1", matched)
	}

	var found bool
	var untouchedCount int
	if err := o.Walk(func(_ string, d ocispec.Descriptor) error {
		ref := d.Annotations[ocispec.AnnotationRefName]
		if ref == "example.com/repo1:renamed" {
			found = true
		}
		if ref == "example.com/repo0:tag0" || ref == "example.com/repo2:tag2" {
			untouchedCount++
		}
		if ref == target {
			t.Errorf("old reference %q still present after UpdateAnnotations", target)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !found {
		t.Error("renamed reference not found after UpdateAnnotations")
	}
	if untouchedCount != 2 {
		t.Errorf("untouched entries = %d, want 2", untouchedCount)
	}
}

// TestOCI_UpdateAnnotationsNoMatchDoesNotWrite verifies that when no
// descriptor matches, UpdateAnnotations returns (0, nil) and does not touch
// index.json on disk at all -- not even a no-op rewrite.
func TestOCI_UpdateAnnotationsNoMatchDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	if err := o.AddIndex(refDescriptor(0)); err != nil {
		t.Fatalf("seed AddIndex: %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)
	old := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(indexPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	matched, err := o.UpdateAnnotations(
		func(d ocispec.Descriptor) bool { return false },
		func(a map[string]string) { a["should-not-be-called"] = "true" },
	)
	if err != nil {
		t.Fatalf("UpdateAnnotations: %v", err)
	}
	if matched != 0 {
		t.Fatalf("matched = %d, want 0", matched)
	}

	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("index.json mtime changed on a zero-match UpdateAnnotations call: got %v, want %v", info.ModTime(), old)
	}
}

// TestOCI_ConcurrentUpdateAnnotations runs many concurrent UpdateAnnotations
// calls, each targeting a distinct descriptor, and confirms no race/panic
// and that all renames land.
func TestOCI_ConcurrentUpdateAnnotations(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	const n = 20
	for i := 0; i < n; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			old := fmt.Sprintf("example.com/repo%d:tag%d", i, i)
			_, err := o.UpdateAnnotations(
				func(d ocispec.Descriptor) bool {
					return d.Annotations[ocispec.AnnotationRefName] == old
				},
				func(a map[string]string) {
					a[ocispec.AnnotationRefName] = fmt.Sprintf("example.com/repo%d:renamed", i)
				},
			)
			if err != nil {
				t.Errorf("UpdateAnnotations(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	renamed := make(map[string]bool)
	if err := o.Walk(func(_ string, d ocispec.Descriptor) error {
		renamed[d.Annotations[ocispec.AnnotationRefName]] = true
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("example.com/repo%d:renamed", i)
		if !renamed[want] {
			t.Errorf("missing renamed entry %q after concurrent UpdateAnnotations", want)
		}
	}
}

// --------------------------------------------------------------------------
// TestOCI_AddIndexSkipsSaveWhenUnchanged
// --------------------------------------------------------------------------

// TestOCI_AddIndexSkipsSaveWhenUnchanged verifies the "cheap win" dedup: a
// second AddIndex call with a byte-identical descriptor must not rewrite
// index.json (checked via mtime, since content alone can't distinguish a
// skip from an identical rewrite). A subsequent AddIndex with a genuinely
// different descriptor (different Size) for the same key must still write.
func TestOCI_AddIndexSkipsSaveWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	desc := refDescriptor(0)
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex (first): %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)
	old := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(indexPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Re-add the exact same descriptor (must be treated as unchanged).
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex (repeat, unchanged): %v", err)
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("index.json was rewritten for a byte-identical AddIndex: mtime got %v, want unchanged %v", info.ModTime(), old)
	}

	// Now add a genuinely different descriptor for the same key (different
	// Size) -- this must write.
	changed := desc
	changed.Size = desc.Size + 1
	if err := o.AddIndex(changed); err != nil {
		t.Fatalf("AddIndex (changed): %v", err)
	}
	info2, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat (after changed): %v", err)
	}
	if info2.ModTime().Equal(old) {
		t.Error("index.json mtime unchanged after a genuinely different AddIndex; expected a write")
	}
}
