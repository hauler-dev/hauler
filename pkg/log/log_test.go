package log

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewLogger_WritesToProvidedWriter proves NewLogger honors its out
// io.Writer parameter instead of hardcoding os.Stdout. A logger built over a
// *bytes.Buffer must write its log lines into that buffer, not to the real
// process stdout.
func TestNewLogger_WritesToProvidedWriter(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Infof("hello %s", "world")

	got := buf.String()
	if !strings.Contains(got, "hello world") {
		t.Errorf("NewLogger(&buf).Infof did not write into the provided buffer; buf = %q", got)
	}
}

// TestNewLogger_NoAnsiEscapesUnderNonTerminal is the direct regression test
// for the bug where zerolog.ConsoleWriter emitted ANSI color codes
// unconditionally, regardless of whether the destination was a terminal.
// go test's real os.Stdout is never a terminal, so ShouldUseColor() must
// evaluate false here and NewLogger's ConsoleWriter must set NoColor,
// leaving zero "\x1b[" bytes in the output.
func TestNewLogger_NoAnsiEscapesUnderNonTerminal(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)

	l.Infof("hello")

	got := buf.Bytes()
	if bytes.Contains(got, []byte("\x1b[")) {
		t.Errorf("NewLogger(&buf).Infof produced ANSI escape codes under go test's non-terminal stdout; buf = %q", got)
	}
}
