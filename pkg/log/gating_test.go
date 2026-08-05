package log

import "testing"

// TestShouldShowProgress covers every disabling branch of shouldShowProgress
// plus the "would otherwise be true" baseline. isTerminal is passed directly
// so these tests bypass the real isatty check; NO_COLOR/TERM are read from
// the environment by shouldShowProgress itself, so tests that exercise those
// branches set them via t.Setenv and leave isTerminal true so only the
// branch under test can produce a false result.
func TestShouldShowProgress(t *testing.T) {
	tests := []struct {
		name       string
		isTerminal bool
		noProgress bool
		logLevel   string
		noColor    string
		term       string
		want       bool
	}{
		{
			name:       "baseline: terminal, not disabled, non-debug level",
			isTerminal: true,
			noProgress: false,
			logLevel:   "info",
			term:       "xterm-256color",
			want:       true,
		},
		{
			name:       "noProgress disables regardless of terminal",
			isTerminal: true,
			noProgress: true,
			logLevel:   "info",
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "non-terminal disables",
			isTerminal: false,
			noProgress: false,
			logLevel:   "info",
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "debug log level disables",
			isTerminal: true,
			noProgress: false,
			logLevel:   "debug",
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "debug log level disables regardless of case",
			isTerminal: true,
			noProgress: false,
			logLevel:   "DEBUG",
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "empty log level does not disable",
			isTerminal: true,
			noProgress: false,
			logLevel:   "",
			term:       "xterm-256color",
			want:       true,
		},
		{
			name:       "NO_COLOR set disables",
			isTerminal: true,
			noProgress: false,
			logLevel:   "info",
			noColor:    "1",
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "TERM=dumb disables",
			isTerminal: true,
			noProgress: false,
			logLevel:   "info",
			term:       "dumb",
			want:       false,
		},
		{
			name:       "TERM=DUMB disables regardless of case",
			isTerminal: true,
			noProgress: false,
			logLevel:   "info",
			term:       "DUMB",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("TERM", tt.term)

			if got := shouldShowProgress(tt.isTerminal, tt.noProgress, tt.logLevel); got != tt.want {
				t.Errorf("shouldShowProgress(%v, %v, %q) = %v, want %v", tt.isTerminal, tt.noProgress, tt.logLevel, got, tt.want)
			}
		})
	}
}

// TestShouldShowProgress_RealWrapperNonTerminal proves the exported
// ShouldShowProgress wrapper delegates its isTerminal check to the real
// isatty call. go test's stdout is never a terminal, so it must always
// return false here regardless of the other inputs.
func TestShouldShowProgress_RealWrapperNonTerminal(t *testing.T) {
	if got := ShouldShowProgress(false, "info"); got != false {
		t.Errorf("ShouldShowProgress(false, \"info\") under go test (non-terminal stdout) = %v, want false", got)
	}
}

// TestIsDumbSink covers every branch of the shared isTerminal/NO_COLOR/
// TERM=dumb predicate that both shouldShowProgress and ShouldUseColor build
// on. isTerminal is passed directly to bypass the real isatty check;
// NO_COLOR/TERM are read from the environment by isDumbSink itself.
func TestIsDumbSink(t *testing.T) {
	tests := []struct {
		name       string
		isTerminal bool
		noColor    string
		term       string
		want       bool
	}{
		{
			name:       "terminal, no NO_COLOR, non-dumb TERM is not a dumb sink",
			isTerminal: true,
			term:       "xterm-256color",
			want:       false,
		},
		{
			name:       "non-terminal is a dumb sink",
			isTerminal: false,
			term:       "xterm-256color",
			want:       true,
		},
		{
			name:       "NO_COLOR set is a dumb sink even on a terminal",
			isTerminal: true,
			noColor:    "1",
			term:       "xterm-256color",
			want:       true,
		},
		{
			name:       "TERM=dumb is a dumb sink even on a terminal",
			isTerminal: true,
			term:       "dumb",
			want:       true,
		},
		{
			name:       "TERM=DUMB is a dumb sink regardless of case",
			isTerminal: true,
			term:       "DUMB",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("TERM", tt.term)

			if got := isDumbSink(tt.isTerminal); got != tt.want {
				t.Errorf("isDumbSink(%v) = %v, want %v", tt.isTerminal, got, tt.want)
			}
		})
	}
}

// TestShouldUseColor_RealWrapperNonTerminal is the ShouldUseColor analogue
// of TestShouldShowProgress_RealWrapperNonTerminal: go test's real stdout is
// never a terminal, so ShouldUseColor must always report false here.
func TestShouldUseColor_RealWrapperNonTerminal(t *testing.T) {
	if got := ShouldUseColor(); got != false {
		t.Errorf("ShouldUseColor() under go test (non-terminal stdout) = %v, want false", got)
	}
}

// TestShouldUseColor_DebugLevelStillUsesColor proves colors are not gated on
// log level -- unlike shouldShowProgress, ShouldUseColor has no notion of
// logLevel at all, so debug-level logging on a real terminal should still be
// colored. This is exercised indirectly: ShouldUseColor takes no logLevel
// parameter, so there is nothing for a "debug" value to disable. This test
// documents that omission is intentional by asserting the function's
// isDumbSink-only behavior via a terminal=true case.
func TestShouldUseColor_TerminalWithoutDumbSignalsIsTrue(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	if got := isDumbSink(true); got != false {
		t.Errorf("isDumbSink(true) with no NO_COLOR/TERM=dumb = %v, want false (i.e. ShouldUseColor would be true on a real non-dumb terminal)", got)
	}
}
