package pcm

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestBigEndianInt16DecodedAndEncoded(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatInt16BE, Layout: LayoutMono}
	f64 := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}

	c, err := NewConverter(in, f64)
	if err != nil {
		t.Fatal(err)
	}
	got := convertChunks(t, c, []byte{0x12, 0x34}, 1)
	v := math.Float64frombits(binary.LittleEndian.Uint64(got))
	want := float64(int16(0x1234)) / 32767
	if math.Abs(v-want) > 1e-12 {
		t.Fatalf("BE decode = %.12g, want %.12g", v, want)
	}

	c2, err := NewConverter(f64, in)
	if err != nil {
		t.Fatal(err)
	}
	encoded := convertChunks(t, c2, got, 3)
	if string(encoded) != string([]byte{0x12, 0x34}) {
		t.Fatalf("BE encode = % x", encoded)
	}
}

func TestInt24SignExtension(t *testing.T) {
	cases := []struct {
		name string
		sf   SampleFormat
		raw  []byte
		want int32
	}{
		{"le-positive", FormatInt24LE, []byte{0x34, 0x12, 0x7f}, 0x7f1234},
		{"le-negative", FormatInt24LE, []byte{0xff, 0xff, 0xff}, -1},
		{"be-positive", FormatInt24BE, []byte{0x7f, 0x12, 0x34}, 0x7f1234},
		{"be-negative", FormatInt24BE, []byte{0xff, 0xff, 0xff}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: tc.sf, Layout: LayoutMono}
			out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
			c, err := NewConverter(in, out)
			if err != nil {
				t.Fatal(err)
			}
			got := convertChunks(t, c, tc.raw, 2)
			v := math.Float64frombits(binary.LittleEndian.Uint64(got))
			var want float64
			if tc.want >= 0 {
				want = float64(tc.want) / 8388607
			} else {
				want = float64(tc.want) / 8388608
			}
			if v != want {
				t.Fatalf("decoded = %.12g, want %.12g", v, want)
			}
		})
	}
}

func TestUint8Offset(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatUint8, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatUint8, Layout: LayoutMono}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte{0, 128, 255}
	got := convertChunks(t, c, input, 1)
	if string(got) != string(input) {
		t.Fatalf("u8 identity = %v", got)
	}
}
