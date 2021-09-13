package registry

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type ErrorCode int

const (
	StatusBlobUnknown ErrorCode = iota
	StatusBlobInvalid

	StatusDigestInvalid

	StatusNameInvalid
	StatusNameUnknown

	StatusReferenceInvalid
	StatusReferenceUnknown

	StatusTagInvalid
	StatusTagUnknown
)

type Error struct {
	Status  ErrorCode
	Code    string
	Message string
}

func NewError(code ErrorCode, msg ...interface{}) Error {
	var message string
	switch code {
	case StatusBlobUnknown:
		message = fmt.Sprintf("Blob unknown: %v", msg)
	case StatusBlobInvalid:
		message = fmt.Sprintf("Invalid blob: %v", msg)
	}

	return Error{
		Status: code,
		Message: message,
	}
}


func (e Error) Write(c *fiber.Ctx) error {
	c.Status(int(e.Status))
	return nil
}
