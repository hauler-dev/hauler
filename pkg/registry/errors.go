package registry

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
