package log

import (
	"github.com/rs/zerolog"
	"os"
)

type Logger interface {
	Info() *zerolog.Event
	Debug() *zerolog.Event
	Error() *zerolog.Event
}

func NewPrettyLogger() zerolog.Logger {
	output := zerolog.ConsoleWriter{
		Out: os.Stdout,
	}

	logger := zerolog.New(output).With().Timestamp().Logger()
	return logger
}