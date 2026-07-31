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

// TestCaptureOutput_Concurrent documents why captureMu exists in CaptureOutput:
// concurrent calls swap the process-global os.Stdout/os.Stderr, and cosign v3's
// verify path also touches sigstore package-level TUF/Fulcio state that isn't
// documented as concurrency-safe. Without serializing the whole function body,
// concurrent CaptureOutput calls can race on those globals and leave
// os.Stdout/os.Stderr pointing at a closed pipe instead of the original files.
// Do not "simplify" this test away, and do not narrow captureMu's critical
// section to just the redirect/restore lines.
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
