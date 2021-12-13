package log

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Logger provides an interface for all used logger features regardless of logging backend
type Logger interface {
	SetLevel(string)
	With(Fields) *logger
	WithContext(context.Context) context.Context

	Errorf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Debugf(string, ...interface{})
}

type logger struct {
	zl zerolog.Logger
}

// Fields defines fields to attach to log msgs
type Fields map[string]string

// NewLogger returns a new Logger
func NewLogger(out io.Writer) Logger {
	output := zerolog.ConsoleWriter{Out: os.Stdout}
	l := log.Output(output)
	return &logger{
		zl: l.With().Timestamp().Logger(),
	}
}

// FromContext returns a Logger from a context if it exists
func FromContext(ctx context.Context) Logger {
	zl := zerolog.Ctx(ctx)
	return &logger{
		zl: *zl,
	}
}

// SetLevel sets the global log level
func (l *logger) SetLevel(level string) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl, _ = zerolog.ParseLevel("info")
	}

	zerolog.SetGlobalLevel(lvl)
}

// WithContext stores the Logger in the given context and returns it
func (l *logger) WithContext(ctx context.Context) context.Context {
	return l.zl.WithContext(ctx)
}

// With attaches Fields to a Logger
func (l *logger) With(fields Fields) *logger {
	zl := l.zl.With()
	for k, v := range fields {
		zl = zl.Str(k, v)
	}

	return &logger{
		zl: zl.Logger(),
	}
}

// Errorf prints a formatted ERR message
func (l *logger) Errorf(format string, args ...interface{}) {
	l.zl.Error().Msgf(format, args...)
}

// Infof prints a formatted INFO message
func (l *logger) Infof(format string, args ...interface{}) {
	l.zl.Info().Msgf(format, args...)
}

// Warnf prints a formatted WARN message
func (l *logger) Warnf(format string, args ...interface{}) {
	l.zl.Warn().Msgf(format, args...)
}

// Debugf prints a formatted DBG message
func (l *logger) Debugf(format string, args ...interface{}) {
	l.zl.Debug().Msgf(format, args...)
}
