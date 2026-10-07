package pcm

import "fmt"

// SampleFormat describes one raw PCM sample's integer/float encoding,
// bit depth and byte order.
//
// The package supports interleaved, packed PCM only. There is no padding
// between samples. Uint8 samples use the unsigned WAV convention [0,255];
// all larger integer formats use signed two's-complement samples.
type SampleFormat uint8

const (
	// FormatUnknown is never valid and is rejected.
	FormatUnknown SampleFormat = iota
	FormatUint8
	FormatInt16LE
	FormatInt16BE
	FormatInt24LE
	FormatInt24BE
	FormatInt32LE
	FormatInt32BE
	FormatFloat32LE
	FormatFloat32BE
	FormatFloat64LE
	FormatFloat64BE
)

// String returns a stable human-readable format name.
func (f SampleFormat) String() string {
	switch f {
	case FormatUint8:
		return "u8"
	case FormatInt16LE:
		return "s16le"
	case FormatInt16BE:
		return "s16be"
	case FormatInt24LE:
		return "s24le"
	case FormatInt24BE:
		return "s24be"
	case FormatInt32LE:
		return "s32le"
	case FormatInt32BE:
		return "s32be"
	case FormatFloat32LE:
		return "f32le"
	case FormatFloat32BE:
		return "f32be"
	case FormatFloat64LE:
		return "f64le"
	case FormatFloat64BE:
		return "f64be"
	default:
		return "unknown"
	}
}

// Valid reports whether f is a supported packed PCM sample format.
func (f SampleFormat) Valid() bool {
	switch f {
	case FormatUint8,
		FormatInt16LE, FormatInt16BE,
		FormatInt24LE, FormatInt24BE,
		FormatInt32LE, FormatInt32BE,
		FormatFloat32LE, FormatFloat32BE,
		FormatFloat64LE, FormatFloat64BE:
		return true
	default:
		return false
	}
}

func (f SampleFormat) valid() bool { return f.Valid() }

// Bits is the number of significant bits per sample.
func (f SampleFormat) Bits() int {
	switch f {
	case FormatUint8:
		return 8
	case FormatInt16LE, FormatInt16BE:
		return 16
	case FormatInt24LE, FormatInt24BE:
		return 24
	case FormatInt32LE, FormatInt32BE, FormatFloat32LE, FormatFloat32BE:
		return 32
	case FormatFloat64LE, FormatFloat64BE:
		return 64
	default:
		return 0
	}
}

func (f SampleFormat) bits() int { return f.Bits() }

// BytesPerSample is the packed storage size of one sample.
func (f SampleFormat) BytesPerSample() int {
	if b := f.Bits(); b > 0 {
		return b / 8
	}
	return 0
}

func (f SampleFormat) bytesPerSample() int { return f.BytesPerSample() }

// Floating reports whether f is an IEEE-754 floating-point representation.
func (f SampleFormat) Floating() bool {
	switch f {
	case FormatFloat32LE, FormatFloat32BE, FormatFloat64LE, FormatFloat64BE:
		return true
	default:
		return false
	}
}

func (f SampleFormat) floating() bool { return f.Floating() }

func (f SampleFormat) bigEndian() bool {
	switch f {
	case FormatInt16BE, FormatInt24BE, FormatInt32BE, FormatFloat32BE, FormatFloat64BE:
		return true
	default:
		return false
	}
}

// PCMFormat completely describes one interleaved PCM stream.
type PCMFormat struct {
	SampleRate   int
	Channels     int
	SampleFormat SampleFormat
	// Layout must be one of LayoutMono, LayoutStereo, Layout51 or Layout71
	// and its channel count must equal Channels.
	Layout ChannelLayout
}

func (f PCMFormat) frameBytes() int {
	return f.Channels * f.SampleFormat.bytesPerSample()
}

func (f PCMFormat) validate() error {
	if !f.SampleFormat.valid() {
		return newStreamError(ErrorUnsupportedFormat, 0, -1, "unsupported sample format")
	}
	if f.SampleRate <= 0 || f.SampleRate > 1_000_000 {
		return newStreamError(ErrorUnsupportedFormat, 0, -1,
			"sample rate must be in [1,1000000]")
	}
	if f.Channels <= 0 {
		return newStreamError(ErrorInvalidConfig, 0, -1, "channel count must be positive")
	}
	want, ok := canonicalLayout(f.Channels)
	if !ok {
		return newStreamError(ErrorUnsupportedFormat, 0, -1,
			fmt.Sprintf("unsupported channel count: %d", f.Channels))
	}
	if !layoutsEqual(f.Layout, want) {
		return newStreamError(ErrorUnsupportedFormat, 0, -1,
			fmt.Sprintf("channel layout must be %s for %d channels", want.name, f.Channels))
	}
	if f.SampleFormat.bytesPerSample() == 0 {
		return newStreamError(ErrorUnsupportedFormat, 0, -1, "sample format has zero storage size")
	}
	return nil
}

// Channel identifies a speaker position in an interleaved PCM layout.
type Channel uint8

const (
	ChannelUnknown Channel = iota
	ChannelFrontLeft
	ChannelFrontRight
	ChannelFrontCenter
	ChannelLowFrequency
	ChannelBackLeft
	ChannelBackRight
	ChannelSideLeft
	ChannelSideRight
)

// ChannelLayout is an ordered interleaved channel layout.
type ChannelLayout struct {
	name     string
	channels []Channel
}

// Name returns the conventional layout name.
func (l ChannelLayout) Name() string { return l.name }

// Count returns the number of channels in the layout.
func (l ChannelLayout) Count() int { return len(l.channels) }

func (l ChannelLayout) channelCount() int { return l.Count() }

// Channels returns a copy of the ordered speaker-position list.
func (l ChannelLayout) Channels() []Channel {
	out := make([]Channel, len(l.channels))
	copy(out, l.channels)
	return out
}

// Equal reports whether two layouts use the same ordered channels.
func (l ChannelLayout) Equal(other ChannelLayout) bool { return layoutsEqual(l, other) }

var (
	LayoutMono = ChannelLayout{
		name:     "mono",
		channels: []Channel{ChannelFrontCenter},
	}
	LayoutStereo = ChannelLayout{
		name:     "stereo",
		channels: []Channel{ChannelFrontLeft, ChannelFrontRight},
	}
	Layout51 = ChannelLayout{
		name: "5.1",
		channels: []Channel{ChannelFrontLeft, ChannelFrontRight, ChannelFrontCenter,
			ChannelLowFrequency, ChannelBackLeft, ChannelBackRight},
	}
	Layout71 = ChannelLayout{
		name: "7.1",
		channels: []Channel{ChannelFrontLeft, ChannelFrontRight, ChannelFrontCenter,
			ChannelLowFrequency, ChannelBackLeft, ChannelBackRight,
			ChannelSideLeft, ChannelSideRight},
	}
)

func canonicalLayout(channels int) (ChannelLayout, bool) {
	switch channels {
	case 1:
		return LayoutMono, true
	case 2:
		return LayoutStereo, true
	case 6:
		return Layout51, true
	case 8:
		return Layout71, true
	default:
		return ChannelLayout{}, false
	}
}

func layoutsEqual(a, b ChannelLayout) bool {
	if len(a.channels) != len(b.channels) {
		return false
	}
	for i := range a.channels {
		if a.channels[i] != b.channels[i] {
			return false
		}
	}
	return true
}
