package log

import (
	"github.com/sirupsen/logrus"
	"io"
)

type Logger interface {
	InvalidArg(string)

	Errorf(string, ...interface{})
	Infof(string, ...interface{})
	Warnf(string, ...interface{})
	Debugf(string, ...interface{})
}

type standardLogger struct {
	*logrus.Logger
}

type Event struct {
	id      int
	message string
}

var (
	invalidArgMessage = Event{1, "Invalid arg: %s"}
)

func NewLogger(out io.Writer) *standardLogger {
	logger := logrus.New()
	logger.SetOutput(out)

	return &standardLogger{logger}
}

func (l *standardLogger) InvalidArg(arg string) {
	l.Errorf(invalidArgMessage.message, arg)
}
