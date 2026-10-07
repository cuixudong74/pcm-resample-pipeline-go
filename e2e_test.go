package pcm

import (
	"bytes"
	"math"
	"testing"
)

func TestEndToEndStereoFloat64ToMonoInt16Resampled(t *testing.T) {
	in := PCMFormat{SampleRate: 48000, Channels: 2, SampleFormat: FormatFloat64LE, Layout: LayoutStereo}
	out := PCMFormat{SampleRate: 44100, Channels: 1, SampleFormat: FormatInt16LE, Layout: LayoutMono}
	c, err := NewConverter(in, out, WithSincResampling(4, 0), WithDither(DitherNone, 0))
	if err != nil {
		t.Fatal(err)
	}

	frames := 50
	var input []byte
	for n := 0; n < frames; n++ {
		l := 0.5 * math.Sin(2*math.Pi*440*float64(n)/48000)
		r := -l
		for _, v := range []float64{l, r} {
			var b [8]byte
			bits := math.Float64bits(v)
			for i := range b {
				b[i] = byte(bits >> (8 * i))
			}
			input = append(input, b[:]...)
		}
	}
	reference := convertChunks(t, c, input, len(input))
	wantFrames := (frames*44100 + 48000 - 1) / 48000
	if len(reference) != wantFrames*2 {
		t.Fatalf("length = %d, want %d", len(reference), wantFrames*2)
	}
	for _, size := range []int{1, 8, 15, 31, 128, len(input) + 100} {
		got := convertChunks(t, c, input, size)
		if !bytes.Equal(got, reference) {
			t.Fatalf("end-to-end result differs for chunk size %d", size)
		}
	}
}

func TestAllSupportedSampleRepresentationsConstructible(t *testing.T) {
	formats := []SampleFormat{
		FormatUint8,
		FormatInt16LE, FormatInt16BE,
		FormatInt24LE, FormatInt24BE,
		FormatInt32LE, FormatInt32BE,
		FormatFloat32LE, FormatFloat32BE,
		FormatFloat64LE, FormatFloat64BE,
	}
	for _, sf := range formats {
		in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: sf, Layout: LayoutMono}
		out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
		c, err := NewConverter(in, out)
		if err != nil {
			t.Fatalf("format %s: %v", sf, err)
		}
		data := make([]byte, sf.BytesPerSample())
		// Use midpoint/zero for every representation.
		if sf == FormatUint8 {
			data[0] = 128
		}
		got := convertChunks(t, c, data, 1)
		if len(got) != 8 {
			t.Fatalf("format %s output length = %d", sf, len(got))
		}
		v := math.Float64frombits(binaryLittleEndianUint64(got))
		if v != 0 {
			t.Fatalf("format %s midpoint decoded as %.12g", sf, v)
		}
	}
}

func binaryLittleEndianUint64(b []byte) uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}
