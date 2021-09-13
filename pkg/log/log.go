package log

import (
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Logger interface {
	Errorf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Debugf(string, ...interface{})
}

type logger struct {
	//TODO: Actually check this
	level string

	l zerolog.Logger
}

type Event struct {
	id      int
	message string
}

var (
	invalidArgMessage = Event{1, "Invalid arg: %s"}
)

func NewLogger(out io.Writer) *logger {
	l := log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	return &logger{
		l: l.With().Timestamp().Logger(),
	}
}

func (l *logger) With() zerolog.Context {
	return l.l.With()
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.l.Error().Msgf(format, args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.l.Info().Msgf(format, args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.l.Warn().Msgf(format, args...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.l.Debug().Msgf(format, args...)
}

func (l *logger) InvalidArg(arg string) {
	l.Errorf(invalidArgMessage.message, arg)
}
