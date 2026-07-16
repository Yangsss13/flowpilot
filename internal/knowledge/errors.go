package knowledge

import "errors"

var (
	ErrInvalidInput = errors.New("invalid knowledge input")
	ErrNotFound     = errors.New("knowledge resource not found")
	ErrConflict     = errors.New("knowledge state conflict")
	ErrUnavailable  = errors.New("knowledge service unavailable")
	ErrMediaInvalid = errors.New("invalid media")
	ErrMediaRuntime = errors.New("media runtime unavailable")
	ErrMediaTimeout = errors.New("media processing timed out")
)
