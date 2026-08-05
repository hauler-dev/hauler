package content

import (
	"errors"
	"testing"
)

// closeTrackingWriter records whether Close was called and can be made to
// return an error from Write and/or Close for testing IoContentWriter.Close's
// error-combination behavior.
type closeTrackingWriter struct {
	closed    bool
	closeErr  error
	writeErr  error
	writeData []byte
}

func (w *closeTrackingWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	w.writeData = append(w.writeData, p...)
	return len(p), nil
}

func (w *closeTrackingWriter) Close() error {
	w.closed = true
	return w.closeErr
}

// TestIoContentWriter_Close_DigestMismatchStillClosesUnderlyingWriter verifies
// that when the computed digest doesn't match the expected output hash,
// Close() still closes the underlying writer (no fd leak) even though it
// returns a digest-mismatch error.
func TestIoContentWriter_Close_DigestMismatchStillClosesUnderlyingWriter(t *testing.T) {
	underlying := &closeTrackingWriter{}
	w := NewIoContentWriter(underlying, WithOutputHash("sha256:doesnotmatch"))

	if _, err := w.Write([]byte("some data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := w.Close()
	if err == nil {
		t.Fatal("Close: expected digest mismatch error, got nil")
	}
	if !underlying.closed {
		t.Error("Close: underlying writer was not closed on digest mismatch (fd leak)")
	}
}

// TestIoContentWriter_Close_NoMismatchClosesAndReturnsNil verifies the happy
// path still works: matching digest closes the underlying writer and returns
// no error.
func TestIoContentWriter_Close_NoMismatchClosesAndReturnsNil(t *testing.T) {
	underlying := &closeTrackingWriter{}
	w := NewIoContentWriter(underlying)

	data := []byte("some data")
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// No outputHash configured, so no verification should occur.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	if !underlying.closed {
		t.Error("Close: underlying writer was not closed")
	}
}

// TestIoContentWriter_Close_ReturnsCloseErrorWhenNoMismatch verifies that a
// close error from the underlying writer is surfaced when there is no digest
// mismatch to report.
func TestIoContentWriter_Close_ReturnsCloseErrorWhenNoMismatch(t *testing.T) {
	wantErr := errors.New("boom: close failed")
	underlying := &closeTrackingWriter{closeErr: wantErr}
	w := NewIoContentWriter(underlying)

	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := w.Close()
	if err == nil {
		t.Fatal("Close: expected close error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Close: error %v does not wrap %v", err, wantErr)
	}
	if !underlying.closed {
		t.Error("Close: underlying writer was not closed")
	}
}
