package content

import (
	"fmt"
	"os"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// addTestIndexEntry adds a uniquely-named descriptor through AddIndex.
func addTestIndexEntry(t *testing.T, o *OCI, i int) {
	t.Helper()
	desc := ocispec.Descriptor{
		MediaType: consts.OCIManifestSchema1,
		Digest:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Size:      1,
		Annotations: map[string]string{
			ocispec.AnnotationRefName: fmt.Sprintf("hauler/test-%d:latest", i),
			consts.KindAnnotationName: consts.KindAnnotationImage,
		},
	}
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex(%d): %v", i, err)
	}
}

func TestAddIndexCheckpointsOnInterval(t *testing.T) {
	// newTestOCI calls LoadIndex, which is required before AddIndex can be
	// called directly against a bare OCI -- see newTestOCI's doc comment in
	// oci_concurrency_test.go for why o.index must be non-nil first.
	o := newTestOCI(t, t.TempDir())

	// Fixed clock: no time passes unless the test advances it.
	clock := time.Now()
	o.now = func() time.Time { return clock }

	for i := 0; i < 25; i++ {
		addTestIndexEntry(t, o, i)
	}

	st := o.Stats().Snapshot()
	if st.IndexWrites != 25 {
		t.Fatalf("IndexWrites = %d, want 25", st.IndexWrites)
	}
	if st.IndexDurableWrites != 1 {
		t.Fatalf("IndexDurableWrites = %d, want 1 (only the first save of a run)", st.IndexDurableWrites)
	}

	// Advance past the interval: the next save must be durable again.
	clock = clock.Add(indexCheckpointInterval + time.Second)
	addTestIndexEntry(t, o, 25)

	st = o.Stats().Snapshot()
	if st.IndexDurableWrites != 2 {
		t.Fatalf("IndexDurableWrites = %d after advancing the clock, want 2", st.IndexDurableWrites)
	}
}

func TestSaveIndexIsAlwaysDurable(t *testing.T) {
	o := newTestOCI(t, t.TempDir())
	clock := time.Now()
	o.now = func() time.Time { return clock }

	for i := 0; i < 3; i++ {
		if err := o.SaveIndex(); err != nil {
			t.Fatalf("SaveIndex: %v", err)
		}
	}

	st := o.Stats().Snapshot()
	if st.IndexDurableWrites != 3 {
		t.Fatalf("IndexDurableWrites = %d, want 3 (SaveIndex ignores the interval)", st.IndexDurableWrites)
	}
}

// TestCheckpointPathsProduceIdenticalIndex verifies the durable and
// non-durable paths differ only in fsync -- the bytes on disk must match.
func TestCheckpointPathsProduceIdenticalIndex(t *testing.T) {
	build := func(durable bool) []byte {
		o := newTestOCI(t, t.TempDir())
		addTestIndexEntry(t, o, 1)
		o.lock()
		if err := o.saveIndexLocked(durable); err != nil {
			o.mu.Unlock()
			t.Fatalf("saveIndexLocked(%v): %v", durable, err)
		}
		o.mu.Unlock()

		data, err := os.ReadFile(o.path(ocispec.ImageIndexFile))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		return data
	}

	if a, b := build(true), build(false); string(a) != string(b) {
		t.Fatalf("durable and non-durable index bytes differ:\n durable: %s\n plain:   %s", a, b)
	}
}
