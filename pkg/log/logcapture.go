package log

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// CustomWriter forwards log messages to the application's logger
type CustomWriter struct {
	logger Logger
	level  string
}

func (cw *CustomWriter) Write(p []byte) (n int, err error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		if cw.level == "error" {
			cw.logger.Errorf("%s", message)
		} else if cw.level == "info" {
			cw.logger.Infof("%s", message)
		} else {
			cw.logger.Debugf("%s", message)
		}
	}
	return len(p), nil
}

// logStream reads lines from a reader and writes them to the provided writer
func logStream(reader io.Reader, customWriter *CustomWriter, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		customWriter.Write([]byte(scanner.Text()))
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		customWriter.logger.Errorf("error reading log stream: %v", err)
	}
}

// captureMu serializes the entire body of CaptureOutput, not just the fd swap:
// os.Stdout/os.Stderr are process-global, so overlapping captures would route
// each other's output to the wrong logger and restore the wrong originals.
//
// Its scope is that swap and whatever prints beneath it. The only non-test
// caller left is the Helm chart traversal in runChartJobs
// (cmd/hauler/cli/store/add.go), whose downloader prints from transitive
// dependencies. Signature verification reaches sigstore through
// cosign.Verifier, which returns its result rather than printing the report
// cosign's CLI prints, so it needs no capture -- see Verifier.verifyBundle.
var captureMu sync.Mutex

// CaptureOutput redirects stdout and stderr to custom loggers and executes the provided function
func CaptureOutput(logger Logger, debug bool, fn func() error) error {
	captureMu.Lock()
	defer captureMu.Unlock()

	// Create pipes for capturing stdout and stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Save original stdout and stderr
	origStdout := os.Stdout
	origStderr := os.Stderr

	// Redirect stdout and stderr
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	// Use WaitGroup to wait for logging goroutines to finish
	var wg sync.WaitGroup
	wg.Add(2)

	// Start logging goroutines
	if !debug {
		go logStream(stdoutReader, &CustomWriter{logger: logger, level: "info"}, &wg)
		go logStream(stderrReader, &CustomWriter{logger: logger, level: "error"}, &wg)
	} else {
		go logStream(stdoutReader, &CustomWriter{logger: logger, level: "debug"}, &wg)
		go logStream(stderrReader, &CustomWriter{logger: logger, level: "debug"}, &wg)
	}

	// Run the provided function in a separate goroutine
	fnErr := make(chan error, 1)
	go func() {
		fnErr <- fn()
		stdoutWriter.Close() // Close writers to signal EOF to readers
		stderrWriter.Close()
	}()

	// Wait for logging goroutines to finish
	wg.Wait()

	// Restore original stdout and stderr
	os.Stdout = origStdout
	os.Stderr = origStderr

	// Check for errors from the function
	if err := <-fnErr; err != nil {
		return fmt.Errorf("function execution failed: %w", err)
	}

	return nil
}
