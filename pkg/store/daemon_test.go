package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/moby/moby/client"
)

type fakeDaemon struct {
	daemon.Client
	calls   atomic.Int32
	payload []byte
	err     error
	rc      io.ReadCloser
}

func (f *fakeDaemon) ImageSave(context.Context, []string, ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.rc != nil {
		return f.rc, nil
	}
	return io.NopCloser(bytes.NewReader(f.payload)), nil
}

type closableDaemon struct {
	fakeDaemon
	closed atomic.Int32
}

func (c *closableDaemon) Close() error { c.closed.Add(1); return nil }

type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n > 0 {
		r.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("boom")
}

func dirEntries(t *testing.T, dir string) int {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

func TestSpoolingDaemonClient(t *testing.T) {
	ctx := context.Background()

	t.Run("one underlying save serves many readers", func(t *testing.T) {
		dir := t.TempDir()
		f := &fakeDaemon{payload: []byte("tar-bytes")}
		s := &spoolingDaemonClient{Client: f, dir: dir}

		for i := 0; i < 3; i++ {
			r, err := s.ImageSave(ctx, []string{"img"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(got, f.payload) {
				t.Fatalf("call %d: got %q, err %v", i, got, err)
			}
		}
		if n := f.calls.Load(); n != 1 {
			t.Fatalf("underlying calls = %d, want 1", n)
		}
		if n := dirEntries(t, dir); n != 1 {
			t.Fatalf("spool files in dir = %d, want 1", n)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if n := dirEntries(t, dir); n != 0 {
			t.Fatalf("spool files after Close = %d, want 0", n)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})

	t.Run("underlying error leaves no file", func(t *testing.T) {
		dir := t.TempDir()
		want := errors.New("no daemon")
		s := &spoolingDaemonClient{Client: &fakeDaemon{err: want}, dir: dir}
		if _, err := s.ImageSave(ctx, nil); !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
		if n := dirEntries(t, dir); n != 0 {
			t.Fatalf("files = %d, want 0", n)
		}
	})

	t.Run("copy failure leaves no file and sticks", func(t *testing.T) {
		dir := t.TempDir()
		f := &fakeDaemon{rc: io.NopCloser(&failingReader{n: 4})}
		s := &spoolingDaemonClient{Client: f, dir: dir}
		for i := 0; i < 2; i++ {
			if _, err := s.ImageSave(ctx, nil); err == nil {
				t.Fatalf("call %d: expected error", i)
			}
		}
		if n := dirEntries(t, dir); n != 0 {
			t.Fatalf("files = %d, want 0", n)
		}
		if n := f.calls.Load(); n != 1 {
			t.Fatalf("underlying calls = %d, want 1", n)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("concurrent callers share one save", func(t *testing.T) {
		dir := t.TempDir()
		f := &fakeDaemon{payload: bytes.Repeat([]byte("abc"), 1000)}
		s := &spoolingDaemonClient{Client: f, dir: dir}

		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := s.ImageSave(ctx, []string{"img"})
				if err != nil {
					errs <- err
					return
				}
				defer r.Close()
				got, err := io.ReadAll(r)
				if err != nil || !bytes.Equal(got, f.payload) {
					errs <- errors.New("short or corrupt read")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if n := f.calls.Load(); n != 1 {
			t.Fatalf("underlying calls = %d, want 1", n)
		}
		if n := dirEntries(t, dir); n != 1 {
			t.Fatalf("spool files = %d, want 1", n)
		}
		s.Close()
	})

	t.Run("different images after first save is an error", func(t *testing.T) {
		dir := t.TempDir()
		s := &spoolingDaemonClient{Client: &fakeDaemon{payload: []byte("x")}, dir: dir}
		defer s.Close()
		r, err := s.ImageSave(ctx, []string{"a"})
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		if _, err := s.ImageSave(ctx, []string{"b"}); err == nil {
			t.Fatal("expected error for differing images")
		}
	})

	t.Run("bad dir fails before the daemon is asked", func(t *testing.T) {
		f := &fakeDaemon{payload: []byte("x")}
		s := &spoolingDaemonClient{Client: f, dir: t.TempDir() + "/missing"}
		if _, err := s.ImageSave(ctx, nil); err == nil {
			t.Fatal("expected error")
		}
		if n := f.calls.Load(); n != 0 {
			t.Fatalf("underlying calls = %d, want 0", n)
		}
	})

	t.Run("Close closes the underlying client", func(t *testing.T) {
		dir := t.TempDir()
		c := &closableDaemon{fakeDaemon: fakeDaemon{payload: []byte("x")}}
		s := &spoolingDaemonClient{Client: c, dir: dir}
		r, err := s.ImageSave(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if c.closed.Load() != 1 {
			t.Fatalf("client closed %d times, want 1", c.closed.Load())
		}
		if n := dirEntries(t, dir); n != 0 {
			t.Fatalf("spool files = %d, want 0", n)
		}
	})
}
