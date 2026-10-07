package pcm

import "fmt"

// ErrorKind distinguishes failure categories so callers can react
// differently to corrupt data, out-of-range values and truncated frames.
type ErrorKind int

const (
	// ErrKindUnsupported means the requested format combination cannot be handled.
	ErrKindUnsupported ErrorKind = iota
	// ErrKindCorrupt means input bytes could not be decoded (e.g. NaN/Inf float).
	ErrKindCorrupt
	// ErrKindOutOfRange means a sample value exceeds the legal range of its format.
	ErrKindOutOfRange
	// ErrKindIncompleteFrame means the stream ended mid-frame.
	ErrKindIncompleteFrame
)

func (k ErrorKind) String() string {
	switch k {
	case ErrKindUnsupported:
		return "unsupported"
	case ErrKindCorrupt:
		return "corrupt data"
	case ErrKindOutOfRange:
		return "out of range"
	case ErrKindIncompleteFrame:
		return "incomplete frame"
	}
	return "unknown"
}

// Error is the single error type produced by this package. Offset is the
// absolute byte position in the input stream where the problem was found
// (or -1 when not applicable, e.g. unsupported configuration).
type Error struct {
	Kind   ErrorKind
	Offset int64
	Msg    string
}

func (e *Error) Error() string {
	if e.Offset >= 0 {
		return fmt.Sprintf("pcm: %s at input byte %d: %s", e.Kind, e.Offset, e.Msg)
	}
	return fmt.Sprintf("pcm: %s: %s", e.Kind, e.Msg)
}
