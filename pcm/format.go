package pcm

import "fmt"

// SampleFormat describes how a single sample is represented.
type SampleFormat int

const (
	// SampleFormatInt is a signed integer sample (two's complement).
	SampleFormatInt SampleFormat = iota
	// SampleFormatFloat is a floating point sample normalized to [-1, 1].
	SampleFormatFloat
)

// Format describes the layout of an interleaved PCM stream.
// Samples of all channels are interleaved frame by frame, little-endian.
type Format struct {
	SampleRate   int          // samples per second, per channel
	BitDepth     int          // bits per sample: 8/16/24/32 (int), 32/64 (float)
	Channels     int          // number of interleaved channels
	SampleFormat SampleFormat // integer or floating point samples
}

// Validate reports whether the format is supported. Any combination that
// cannot be converted exactly is rejected here instead of silently
// producing wrong samples.
func (f Format) Validate() error {
	if f.SampleRate <= 0 {
		return &Error{Kind: ErrKindUnsupported, Offset: -1,
			Msg: fmt.Sprintf("invalid sample rate %d", f.SampleRate)}
	}
	if f.Channels <= 0 || f.Channels > 64 {
		return &Error{Kind: ErrKindUnsupported, Offset: -1,
			Msg: fmt.Sprintf("invalid channel count %d", f.Channels)}
	}
	switch f.SampleFormat {
	case SampleFormatInt:
		switch f.BitDepth {
		case 8, 16, 24, 32:
		default:
			return &Error{Kind: ErrKindUnsupported, Offset: -1,
				Msg: fmt.Sprintf("unsupported integer bit depth %d (want 8/16/24/32)", f.BitDepth)}
		}
	case SampleFormatFloat:
		switch f.BitDepth {
		case 32, 64:
		default:
			return &Error{Kind: ErrKindUnsupported, Offset: -1,
				Msg: fmt.Sprintf("unsupported float bit depth %d (want 32/64)", f.BitDepth)}
		}
	default:
		return &Error{Kind: ErrKindUnsupported, Offset: -1,
			Msg: fmt.Sprintf("unknown sample format %d", int(f.SampleFormat))}
	}
	return nil
}

// BytesPerSample returns the byte width of a single sample.
func (f Format) BytesPerSample() int { return (f.BitDepth + 7) / 8 }

// FrameSize returns the byte width of one frame (all channels).
func (f Format) FrameSize() int { return f.BytesPerSample() * f.Channels }
