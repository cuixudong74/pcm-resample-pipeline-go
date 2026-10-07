package pcm

import (
	"errors"
	"fmt"
)

// ErrorKind allows callers to distinguish failure modes without parsing text.
type ErrorKind string

const (
	ErrorUnsupportedFormat ErrorKind = "unsupported format"
	ErrorInvalidConfig     ErrorKind = "invalid configuration"
	ErrorCorruptData       ErrorKind = "corrupt data"
	ErrorValueOutOfRange   ErrorKind = "sample value out of range"
	ErrorIncompleteFrame   ErrorKind = "incomplete frame"
	ErrorStreamClosed      ErrorKind = "stream closed"
)

// StreamError is a deterministic PCM stream failure with input location.
//
// Offset is a byte offset measured from the start of the logical input stream
// (including bytes delivered in earlier Append calls). Frame is the zero-based
// frame index near the failure; it is -1 when no frame can be identified.
type StreamError struct {
	Kind   ErrorKind
	Offset int64
	Frame  int64
	Err    error
}

func (e *StreamError) Error() string {
	if e == nil {
		return ""
	}
	detail := ""
	if e.Err != nil {
		detail = ": " + e.Err.Error()
	}
	return fmt.Sprintf("%s at byte %d, frame %d%s", e.Kind, e.Offset, e.Frame, detail)
}

func (e *StreamError) Unwrap() error { return e.Err }

func newStreamError(kind ErrorKind, offset int64, frame int64, message string) *StreamError {
	return &StreamError{
		Kind:   kind,
		Offset: offset,
		Frame:  frame,
		Err:    errors.New(message),
	}
}

func wrapStreamError(kind ErrorKind, offset int64, frame int64, err error) *StreamError {
	return &StreamError{Kind: kind, Offset: offset, Frame: frame, Err: err}
}

// ErrorKindOf reports the kind carried by a StreamError.
// An empty result means the error is not a StreamError.
func ErrorKindOf(err error) ErrorKind {
	var se *StreamError
	if errors.As(err, &se) {
		return se.Kind
	}
	return ""
}
