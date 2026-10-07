package pcm

import (
	"math"
	"testing"
)

func TestIncompleteFrameReportedAtExactTail(t *testing.T) {
	in := fmt16(8000, 2)
	out := fmt16(8000, 2)
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	s := c.NewStream()
	if _, err := s.Append([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append([]byte{2, 3}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Close()
	if err == nil || ErrorKindOf(err) != ErrorIncompleteFrame {
		t.Fatalf("error = %v, want %s", err, ErrorIncompleteFrame)
	}
	se := err.(*StreamError)
	if se.Offset != 0 || se.Frame != 0 {
		t.Fatalf("location = byte %d frame %d", se.Offset, se.Frame)
	}

	// A failed stream is isolated and cannot be continued.
	if _, err := s.Append(nil); ErrorKindOf(err) != ErrorStreamClosed {
		t.Fatalf("reuse error = %v, want %s", err, ErrorStreamClosed)
	}

	// A partial tail following one complete frame is located at the next frame
	// boundary rather than at stream start.
	partial := c.NewStream()
	if _, err := partial.Append(make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	if _, err := partial.Append(make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	_, err = partial.Close()
	if err == nil || ErrorKindOf(err) != ErrorIncompleteFrame {
		t.Fatalf("partial close error = %v", err)
	}
	se = err.(*StreamError)
	if se.Offset != 4 || se.Frame != 1 {
		t.Fatalf("partial location = byte %d frame %d, want byte 4 frame 1",
			se.Offset, se.Frame)
	}

	// A fresh independent stream is unaffected.
	s2 := c.NewStream()
	if _, err := s2.Append([]byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOutOfRangeFloatHasSampleLocation(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat32LE, Layout: LayoutMono}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		bits uint64
		kind ErrorKind
	}{
		{"nan", math.Float64bits(math.NaN()), ErrorCorruptData},
		{"positive-inf", math.Float64bits(math.Inf(1)), ErrorCorruptData},
		{"negative-inf", math.Float64bits(math.Inf(-1)), ErrorCorruptData},
		{"above-one", math.Float64bits(1.0000001), ErrorValueOutOfRange},
		{"below-minus-one", math.Float64bits(-1.0000001), ErrorValueOutOfRange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, 16)
			put := func(off int, v float64) {
				bits := math.Float64bits(v)
				for i := 0; i < 8; i++ {
					buf[off+i] = byte(bits >> (8 * i))
				}
			}
			put(0, 0.25)
			for i := 0; i < 8; i++ {
				buf[8+i] = byte(tc.bits >> (8 * i))
			}
			s := c.NewStream()
			_, err := s.Append(buf)
			if err == nil || ErrorKindOf(err) != tc.kind {
				t.Fatalf("error = %v, want %s", err, tc.kind)
			}
			se := err.(*StreamError)
			if se.Offset != 8 || se.Frame != 1 {
				t.Fatalf("location = byte %d frame %d, want byte 8 frame 1", se.Offset, se.Frame)
			}
		})
	}
}

func TestUnsupportedFormatCombinationsRejectedBeforeStreaming(t *testing.T) {
	cases := []struct {
		name string
		f    PCMFormat
	}{
		{"unknown sample format", PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatUnknown, Layout: LayoutMono}},
		{"zero rate", PCMFormat{SampleRate: 0, Channels: 1, SampleFormat: FormatInt16LE, Layout: LayoutMono}},
		{"three channels", PCMFormat{SampleRate: 8000, Channels: 3, SampleFormat: FormatInt16LE, Layout: ChannelLayout{}}},
		{"mismatched layout", PCMFormat{SampleRate: 8000, Channels: 2, SampleFormat: FormatInt16LE, Layout: LayoutMono}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := fmt16(8000, 1)
			_, err := NewConverter(tc.f, other)
			if err == nil || ErrorKindOf(err) == "" {
				t.Fatalf("error = %v, want StreamError", err)
			}
		})
	}
}

func TestInvalidResamplerConfigRejected(t *testing.T) {
	in := fmt16(8000, 1)
	out := fmt16(16000, 1)
	_, err := NewConverter(in, out, WithSincResampling(0, 1.5))
	if err == nil || ErrorKindOf(err) != ErrorInvalidConfig {
		t.Fatalf("cutoff error = %v", err)
	}
	_, err = NewConverter(in, out, WithSincResampling(2048, 0))
	if err == nil || ErrorKindOf(err) != ErrorInvalidConfig {
		t.Fatalf("width error = %v", err)
	}
}

func TestValidFramesBeforeCorruptFrameAreReturned(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	s := c.NewStream()
	buf := append(float64MonoPCM(0.75), float64MonoPCM(math.NaN())...)
	got, err := s.Append(buf)
	if err == nil || ErrorKindOf(err) != ErrorCorruptData {
		t.Fatalf("error = %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("valid prefix length = %d, want 8", len(got))
	}
	if v := math.Float64frombits(binaryLittleEndianUint64(got)); v != 0.75 {
		t.Fatalf("valid prefix value = %.12g", v)
	}
}

func TestFailedStreamDoesNotAffectOtherStreams(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}

	bad := c.NewStream()
	goodA := c.NewStream()
	goodB := c.NewStream()

	outA, err := goodA.Append(float64MonoPCM(0.25))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Append(float64MonoPCM(math.NaN())); ErrorKindOf(err) != ErrorCorruptData {
		t.Fatalf("bad stream error = %v", err)
	}
	outB, err := goodB.Append(float64MonoPCM(-0.5))
	if err != nil {
		t.Fatalf("goodB after bad failure: %v", err)
	}

	tailA, err := goodA.Close()
	if err != nil {
		t.Fatalf("goodA close: %v", err)
	}
	tailB, err := goodB.Close()
	if err != nil {
		t.Fatalf("goodB close: %v", err)
	}
	outA = append(outA, tailA...)
	outB = append(outB, tailB...)
	if v := math.Float64frombits(binaryLittleEndianUint64(outA)); v != 0.25 {
		t.Fatalf("goodA = %.12g", v)
	}
	if v := math.Float64frombits(binaryLittleEndianUint64(outB)); v != -0.5 {
		t.Fatalf("goodB = %.12g", v)
	}
}
