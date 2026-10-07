package pcm

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func float64MonoPCM(values ...float64) []byte {
	out := make([]byte, len(values)*8)
	for i, v := range values {
		binary.LittleEndian.PutUint64(out[i*8:], math.Float64bits(v))
	}
	return out
}

func TestTPDFDitherDeterministicAndChunkIndependent(t *testing.T) {
	in := PCMFormat{SampleRate: 48000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 48000, Channels: 1, SampleFormat: FormatUint8, Layout: LayoutMono}
	input := float64MonoPCM(0.01, -0.02, 0.03, -0.04, 0.05, -0.06, 0.07, -0.08)

	newC := func(t *testing.T) *Converter {
		t.Helper()
		c, err := NewConverter(in, out, WithDither(DitherTPDF, 12345))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := convertChunks(t, newC(t), input, len(input))
	second := convertChunks(t, newC(t), input, len(input))
	if !bytes.Equal(first, second) {
		t.Fatalf("same seed produced different output: %v != %v", first, second)
	}
	for _, chunk := range []int{1, 3, 8, 17} {
		got := convertChunks(t, newC(t), input, chunk)
		if !bytes.Equal(got, first) {
			t.Fatalf("chunk size %d changed dither sequence: %v != %v", chunk, got, first)
		}
	}

	plain, err := NewConverter(in, out, WithDither(DitherNone, 0))
	if err != nil {
		t.Fatal(err)
	}
	noDither := convertChunks(t, plain, input, len(input))
	if bytes.Equal(noDither, first) {
		t.Fatal("TPDF output unexpectedly equals plain rounded output")
	}
}

func TestDitherRejectedForUpConversionAndFloatOutput(t *testing.T) {
	cases := []struct {
		name string
		in   SampleFormat
		out  SampleFormat
	}{
		{"integer up", FormatInt16LE, FormatInt32LE},
		{"integer same", FormatInt24LE, FormatInt24LE},
		{"integer to float", FormatInt16LE, FormatFloat32LE},
		{"float to float", FormatFloat32LE, FormatFloat64LE},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: tc.in, Layout: LayoutMono}
			out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: tc.out, Layout: LayoutMono}
			_, err := NewConverter(in, out, WithDither(DitherTPDF, 1))
			if err == nil || ErrorKindOf(err) != ErrorInvalidConfig {
				t.Fatalf("error = %v, want %s", err, ErrorInvalidConfig)
			}
		})
	}
}

func TestIntegerUpConversionIsExactAndNoiseless(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatInt16LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatInt32LE, Layout: LayoutMono}
	c, err := NewConverter(in, out, WithDither(DitherTPDF, 99))
	if err == nil {
		t.Fatal("up conversion with TPF should be rejected")
	}
	c, err = NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte{0xff, 0x7f, 0x00, 0x80} // +32767, -32768
	got := convertChunks(t, c, input, 1)
	if len(got) != 8 {
		t.Fatalf("length = %d", len(got))
	}
	u := int32(binary.LittleEndian.Uint32(got[:4]))
	if u != math.MaxInt32 {
		t.Fatalf("positive full-scale upconvert = %d", u)
	}
	u = int32(binary.LittleEndian.Uint32(got[4:]))
	if u != math.MinInt32 {
		t.Fatalf("negative full-scale upconvert = %d", u)
	}
	again := convertChunks(t, c, input, 3)
	if !bytes.Equal(got, again) {
		t.Fatal("up conversion changed with chunk boundary")
	}
}
