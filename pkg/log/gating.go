package log

import (
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// isDumbSink reports whether isTerminal, together with the environment's
// NO_COLOR and TERM=dumb signals, indicates output that should not carry
// ANSI escape codes -- either from the live progress Renderer or from
// zerolog.ConsoleWriter's coloring. This is the single source of truth both
// ShouldShowProgress and ShouldUseColor build on.
func isDumbSink(isTerminal bool) bool {
	if !isTerminal {
		return true
	}
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return true
	}
	return false
}

// shouldShowProgress decides whether the live progress Renderer should be
// used, given the resolved terminal-ness of stdout. It is pure with respect
// to its parameters (isTerminal bypasses the real isatty syscall for tests)
// but still reads NO_COLOR/TERM from the environment via isDumbSink, since
// those aren't plumbed through as CLI inputs.
//
// Rules, in order, first match wins:
//  1. noProgress -> false
//  2. logLevel == "debug" (case-insensitive) -> false: interleaving a live
//     region with verbose debug output is unreadable.
//  3. isDumbSink(isTerminal) (covers !isTerminal, NO_COLOR, TERM=dumb) -> false
//  4. otherwise -> true
func shouldShowProgress(isTerminal bool, noProgress bool, logLevel string) bool {
	if noProgress {
		return false
	}
	if strings.EqualFold(logLevel, "debug") {
		return false
	}
	return !isDumbSink(isTerminal)
}

// ShouldShowProgress is the real-world wrapper around shouldShowProgress,
// resolving terminal-ness via isatty.IsTerminal(os.Stdout.Fd()).
func ShouldShowProgress(noProgress bool, logLevel string) bool {
	return shouldShowProgress(isatty.IsTerminal(os.Stdout.Fd()), noProgress, logLevel)
}

// ShouldUseColor reports whether zerolog.ConsoleWriter should emit ANSI
// color codes, based on the same real-world signals ShouldShowProgress
// consults (isatty on os.Stdout, NO_COLOR, TERM=dumb) -- deliberately NOT
// gated on --no-progress or --log-level debug, since neither of those makes
// colored output undesirable on its own.
func ShouldUseColor() bool {
	return !isDumbSink(isatty.IsTerminal(os.Stdout.Fd()))
}
