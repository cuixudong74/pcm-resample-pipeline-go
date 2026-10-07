package pcm

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func pcmFormat(channels int, sf SampleFormat, rate int) PCMFormat {
	layout := LayoutMono
	switch channels {
	case 2:
		layout = LayoutStereo
	case 6:
		layout = Layout51
	case 8:
		layout = Layout71
	}
	return PCMFormat{SampleRate: rate, Channels: channels, SampleFormat: sf, Layout: layout}
}

func matrix(t *testing.T, inCh, outCh int) [][]float64 {
	t.Helper()
	c, err := newChannelConverter(pcmFormat(inCh, FormatFloat64LE, 8000), pcmFormat(outCh, FormatFloat64LE, 8000))
	if err != nil {
		t.Fatal(err)
	}
	return c.matrix
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestChannelMatrixKnownGains(t *testing.T) {
	m := matrix(t, 1, 2)
	want := 1 / math.Sqrt2
	if !approx(m[0][0], want) || !approx(m[1][0], want) {
		t.Fatalf("mono->stereo = %v", m)
	}

	m = matrix(t, 2, 1)
	if !approx(m[0][0], 0.5) || !approx(m[0][1], 0.5) {
		t.Fatalf("stereo->mono = %v", m[0])
	}

	m = matrix(t, 6, 2)
	// L, C*sqrt(1/2), BL*sqrt(1/2); normalized by 1+sqrt(1/2)+sqrt(1/2)
	direct := 1 / (1 + 2*want)
	cross := want / (1 + 2*want)
	expectedL := []float64{direct, 0, cross, 0, cross, 0}
	expectedR := []float64{0, direct, cross, 0, 0, cross}
	for i, v := range expectedL {
		if !approx(m[0][i], v) {
			t.Fatalf("5.1->stereo L row = %v", m[0])
		}
	}
	for i, v := range expectedR {
		if !approx(m[1][i], v) {
			t.Fatalf("5.1->stereo R row = %v", m[1])
		}
	}

	m = matrix(t, 8, 2)
	direct71 := 1 / (1 + 3*want)
	cross71 := want / (1 + 3*want)
	expectedL = []float64{direct71, 0, cross71, 0, cross71, 0, cross71, 0}
	expectedR = []float64{0, direct71, cross71, 0, 0, cross71, 0, cross71}
	for i, v := range expectedL {
		if !approx(m[0][i], v) {
			t.Fatalf("7.1->stereo L row = %v", m[0])
		}
	}
	for i, v := range expectedR {
		if !approx(m[1][i], v) {
			t.Fatalf("7.1->stereo R row = %v", m[1])
		}
	}

	m = matrix(t, 8, 6)
	lfe := indexOf(Layout71, ChannelLowFrequency)
	lfeOut := indexOf(Layout51, ChannelLowFrequency)
	for i, row := range m {
		if i != lfeOut && row[lfe] != 0 {
			t.Fatalf("5.1 output row %d includes LFE", i)
		}
	}
	if m[lfeOut][lfe] != 1 {
		t.Fatalf("LFE passthrough = %v", m[lfeOut])
	}
	frontLeft := indexOf(Layout51, ChannelFrontLeft)
	sl := indexOf(Layout71, ChannelSideLeft)
	sr := indexOf(Layout71, ChannelSideRight)
	if m[frontLeft][sl] != 0 || m[frontLeft][sr] != 0 {
		t.Fatal("side surround leaked into front channels")
	}
	bl := indexOf(Layout71, ChannelBackLeft)
	if !approx(m[indexOf(Layout51, ChannelBackLeft)][bl], 0.5) ||
		!approx(m[indexOf(Layout51, ChannelBackLeft)][sl], 0.5) {
		t.Fatalf("7.1->5.1 left surround fold = %v", m[indexOf(Layout51, ChannelBackLeft)])
	}
}

func TestChannelConversionStreamNoCrosstalk(t *testing.T) {
	in := pcmFormat(2, FormatFloat64LE, 8000)
	out := pcmFormat(1, FormatFloat64LE, 8000)
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	var input []byte
	values := []float64{1, -1, 0.25, 0.75, -0.4, 0.4}
	for _, v := range values {
		input = append(input, float64MonoPCM(v)...)
	}
	got := convertChunks(t, c, input, 9)
	if len(got) != 3*8 {
		t.Fatalf("length = %d", len(got))
	}
	for frame := 0; frame < 3; frame++ {
		gotV := math.Float64frombits(binary.LittleEndian.Uint64(got[frame*8:]))
		wantV := (values[frame*2] + values[frame*2+1]) / 2
		if !approx(gotV, wantV) {
			t.Fatalf("frame %d = %.12g, want %.12g", frame, gotV, wantV)
		}
	}
}

func TestUpmixSilenceAndChunkIndependence(t *testing.T) {
	in := pcmFormat(1, FormatInt16LE, 8000)
	out := pcmFormat(6, FormatInt16LE, 8000)
	c, err := NewConverter(in, out)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte{0xff, 0x7f}
	reference := convertChunks(t, c, input, len(input))
	for _, size := range []int{1, 2, 3, 4} {
		got := convertChunks(t, c, input, size)
		if !bytes.Equal(got, reference) {
			t.Fatalf("upmix differs for chunk size %d", size)
		}
	}
	if len(reference) != 6*2 {
		t.Fatalf("upmix length = %d", len(reference))
	}
	// First sample maps only to center; all other five channels are zero.
	for channel := 0; channel < 6; channel++ {
		v := int16(binary.LittleEndian.Uint16(reference[channel*2:]))
		if channel == indexOf(Layout51, ChannelFrontCenter) {
			if v != math.MaxInt16 {
				t.Fatalf("center = %d", v)
			}
		} else if v != 0 {
			t.Fatalf("channel %d is not silent: %d", channel, v)
		}
	}
}
