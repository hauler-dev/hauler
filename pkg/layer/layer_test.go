package layer

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
)

// TestFromOpener_OpensExactlyOnce proves FromOpener no longer pays for a
// redundant fetch: compressedOpener is a literal passthrough of
// uncompressedOpener (see FromOpener below), so digest and diffID are
// identical by construction and must be derived from a single read of the
// source, not two.
func TestFromOpener_OpensExactlyOnce(t *testing.T) {
	var opens int32
	data := []byte("hello world")
	opener := func() (io.ReadCloser, error) {
		atomic.AddInt32(&opens, 1)
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	l, err := FromOpener(opener)
	if err != nil {
		t.Fatalf("FromOpener: %v", err)
	}

	if got := atomic.LoadInt32(&opens); got != 1 {
		t.Errorf("expected opener to be invoked exactly once, got %d", got)
	}

	digest, err := l.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	diffID, err := l.DiffID()
	if err != nil {
		t.Fatalf("DiffID: %v", err)
	}
	if digest != diffID {
		t.Errorf("expected digest %v to equal diffID %v (compressedOpener is a passthrough)", digest, diffID)
	}
}
