package log

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Logger interface {
	With(Fields) *logger
	WithContext(context.Context) context.Context
	Errorf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Debugf(string, ...interface{})
}

type logger struct {
	//TODO: Actually check this
	level string

	zl zerolog.Logger
}

type Fields map[string]string

type Event struct {
	id      int
	message string
}

var (
	invalidArgMessage = Event{1, "Invalid arg: %s"}
)

func NewLogger(out io.Writer, level string) *logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl, _ = zerolog.ParseLevel("info")
	}

	zerolog.SetGlobalLevel(lvl)

	l := log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	return &logger{
		zl: l.With().Timestamp().Logger(),
	}
}

func FromContext(ctx context.Context) *logger {
	zl := zerolog.Ctx(ctx)
	return &logger{
		zl: *zl,
	}
}

func (l *logger) WithContext(ctx context.Context) context.Context {
	return l.zl.WithContext(ctx)
}

func (l *logger) With(fields Fields) *logger {
	zl := l.zl.With()
	for k, v := range fields {
		zl = zl.Str(k, v)
	}

	return &logger{
		zl: zl.Logger(),
	}
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.zl.Error().Msgf(format, args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.zl.Info().Msgf(format, args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.zl.Warn().Msgf(format, args...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.zl.Debug().Msgf(format, args...)
}

func (l *logger) InvalidArg(arg string) {
	l.Errorf(invalidArgMessage.message, arg)
}
