package pcm

import (
	"bytes"
	"testing"
)

func fmt16(rate, channels int) PCMFormat {
	layout := LayoutMono
	switch channels {
	case 2:
		layout = LayoutStereo
	case 6:
		layout = Layout51
	case 8:
		layout = Layout71
	}
	return PCMFormat{SampleRate: rate, Channels: channels, SampleFormat: FormatInt16LE, Layout: layout}
}

func convertChunks(t *testing.T, c *Converter, input []byte, chunkSize int) []byte {
	t.Helper()
	var out []byte
	s := c.NewStream()
	for off := 0; off < len(input); off += chunkSize {
		end := off + chunkSize
		if end > len(input) {
			end = len(input)
		}
		got, err := s.Append(input[off:end])
		if err != nil {
			t.Fatalf("Append(%d:%d): %v", off, end, err)
		}
		out = append(out, got...)
	}
	tail, err := s.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	return append(out, tail...)
}

func TestIdentityConversionChunkIndependent(t *testing.T) {
	in := fmt16(48000, 2)
	out := fmt16(48000, 2)
	input := []byte{
		0x00, 0x00, 0xff, 0x7f,
		0x00, 0x80, 0x01, 0x00,
		0xff, 0x7f, 0x00, 0x80,
	}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	reference := convertChunks(t, c, input, len(input))
	if !bytes.Equal(reference, input) {
		t.Fatalf("identity mismatch\n got %v\nwant %v", reference, input)
	}
	for _, size := range []int{1, 2, 3, 4, 5, 7, 64} {
		got := convertChunks(t, c, input, size)
		if !bytes.Equal(got, reference) {
			t.Fatalf("chunk size %d changed output", size)
		}
	}
}

func TestSameRateFloatRoundTrip(t *testing.T) {
	in := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat64LE, Layout: LayoutMono}
	out := PCMFormat{SampleRate: 8000, Channels: 1, SampleFormat: FormatFloat32LE, Layout: LayoutMono}
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	got := convertChunks(t, c, make([]byte, 8), 3)
	if len(got) != 4 {
		t.Fatalf("output length = %d, want 4", len(got))
	}
}
