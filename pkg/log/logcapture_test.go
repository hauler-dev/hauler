package log

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

func discardLogger() Logger {
	l := zerolog.New(io.Discard)
	ctx := l.WithContext(context.Background())
	return FromContext(ctx)
}

// TestCaptureOutput_Concurrent covers why captureMu spans the whole function
// body rather than just the redirect/restore lines: os.Stdout/os.Stderr are
// process-global, so two overlapping captures can interleave their swaps and
// leave the globals pointing at a closed pipe instead of the original files.
func TestCaptureOutput_Concurrent(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr

	logger := discardLogger()

	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = CaptureOutput(logger, false, func() error {
				fmt.Println("hello")
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("CaptureOutput goroutine %d returned error: %v", i, err)
		}
	}

	if os.Stdout != origStdout {
		t.Fatal("os.Stdout was not restored to its original value after concurrent CaptureOutput calls")
	}
	if os.Stderr != origStderr {
		t.Fatal("os.Stderr was not restored to its original value after concurrent CaptureOutput calls")
	}
}
