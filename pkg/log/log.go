package log

import (
	"github.com/pterm/pterm"
	"io"
)

type Logger interface {
	Errorf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Debugf(string, ...interface{})
	Successf(string, ...interface{})
}

type standardLogger struct {
	//TODO: Actually check this
	level string
}

type Event struct {
	id      int
	message string
}

var (
	invalidArgMessage = Event{1, "Invalid arg: %s"}
)

func NewLogger(out io.Writer) *standardLogger {
	return &standardLogger{}
}

func (l *standardLogger) Errorf(format string, args ...interface{}) {
	l.logf("error", format, args...)
}

func (l *standardLogger) Infof(format string, args ...interface{}) {
	l.logf("info", format, args...)
}

func (l *standardLogger) Warnf(format string, args ...interface{}) {
	l.logf("warn", format, args...)
}

func (l *standardLogger) Debugf(format string, args ...interface{}) {
	l.logf("debug", format, args...)
}

func (l *standardLogger) Successf(format string, args ...interface{}) {
	l.logf("success", format, args...)
}

func (l *standardLogger) logf(level string, format string, args ...interface{}) {
	switch level {
	case "debug":
		pterm.Debug.Printfln(format, args...)
	case "info":
		pterm.Info.Printfln(format, args...)
	case "warn":
		pterm.Warning.Printfln(format, args...)
	case "success":
		pterm.Success.Printfln(format, args...)
	default:
		pterm.Error.Printfln("%s is not a valid log level", level)
	}
}

func (l *standardLogger) InvalidArg(arg string) {
	l.Errorf(invalidArgMessage.message, arg)
}
