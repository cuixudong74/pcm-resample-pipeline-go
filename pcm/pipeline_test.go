package pcm

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

// runPipeline feeds data through a fresh pipeline in chunks of `chunk`
// bytes and returns the complete converted output.
func runPipeline(t *testing.T, in, out Format, opts Options, data []byte, chunk int) []byte {
	t.Helper()
	p, err := NewPipeline(in, out, opts)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	var outBuf bytes.Buffer
	tmp := make([]byte, 4096)
	drain := func() {
		for {
			n, rerr := p.Read(tmp)
			outBuf.Write(tmp[:n])
			if n == 0 || rerr == io.EOF {
				return
			}
		}
	}
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		n, werr := p.Write(data[i:end])
		if werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		if n != end-i {
			t.Fatalf("Write consumed %d of %d bytes", n, end-i)
		}
		drain()
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	drain()
	return outBuf.Bytes()
}

// deterministic pseudo-random bytes
func randBytes(seed uint64, n int) []byte {
	b := make([]byte, n)
	s := seed
	for i := range b {
		s += 0x9E3779B97F4A7C15
		z := s
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		b[i] = byte(z >> 32)
	}
	return b
}

func sineFloat32(rate int, freq float64, n int, amp float64) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		v := float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		binary.Write(&buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func TestChunkIndependence(t *testing.T) {
	in := Format{SampleRate: 44100, BitDepth: 16, Channels: 2, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 16, Channels: 2, SampleFormat: SampleFormatInt}
	opts := Options{Quality: QualityMedium, Dither: DitherTPDF, DitherSeed: 42}
	data := randBytes(7, 4410*in.FrameSize()) // 0.1s of stereo

	want := runPipeline(t, in, out, opts, data, len(data)) // all at once
	for _, chunk := range []int{1, 3, 7, 64, 1000, 4093} {
		got := runPipeline(t, in, out, opts, data, chunk)
		if !bytes.Equal(want, got) {
			t.Fatalf("chunk size %d: output differs from one-shot (%d vs %d bytes)", chunk, len(got), len(want))
		}
	}
}

func TestChunkIndependenceNoResample(t *testing.T) {
	in := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 8, Channels: 1, SampleFormat: SampleFormatInt}
	opts := Options{Dither: DitherTPDF, DitherSeed: 1}
	data := randBytes(9, 5000)
	want := runPipeline(t, in, out, opts, data, len(data))
	for _, chunk := range []int{1, 2, 5, 999} {
		got := runPipeline(t, in, out, opts, data, chunk)
		if !bytes.Equal(want, got) {
			t.Fatalf("chunk size %d: output differs", chunk)
		}
	}
}

func TestResampleFrameCount(t *testing.T) {
	cases := []struct{ inRate, outRate, frames int }{
		{44100, 48000, 1000},
		{48000, 16000, 1000},
		{48000, 44100, 4410},
		{8000, 48000, 800},
	}
	for _, c := range cases {
		in := Format{SampleRate: c.inRate, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
		out := Format{SampleRate: c.outRate, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
		data := randBytes(3, c.frames*2)
		got := runPipeline(t, in, out, Options{Quality: QualityHigh}, data, 512)
		wantFrames := (int64(c.frames)*int64(c.outRate) + int64(c.inRate)/2) / int64(c.inRate)
		if int64(len(got))/2 != wantFrames {
			t.Fatalf("%d->%d, %d frames: got %d output frames, want %d",
				c.inRate, c.outRate, c.frames, len(got)/2, wantFrames)
		}
	}
}

func TestResampleSinePreservesFrequency(t *testing.T) {
	in := Format{SampleRate: 48000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	out := Format{SampleRate: 44100, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	const freq = 440.0
	data := sineFloat32(48000, freq, 48000, 0.8) // 1 second
	got := runPipeline(t, in, out, Options{Quality: QualityHigh}, data, 777)

	n := len(got) / 4
	if n != 44100 {
		t.Fatalf("got %d output frames, want 44100", n)
	}
	// count zero crossings in the middle 0.5s to estimate frequency
	cross := 0
	prev := math.Float32frombits(binary.LittleEndian.Uint32(got[10000*4:]))
	maxAmp := float64(0)
	for i := 10001; i < 32000; i++ {
		v := math.Float32frombits(binary.LittleEndian.Uint32(got[i*4:]))
		if a := math.Abs(float64(v)); a > maxAmp {
			maxAmp = a
		}
		if (prev < 0) != (v < 0) {
			cross++
		}
		prev = v
	}
	estFreq := float64(cross) / 2 / 0.5 // crossings over ~0.5s
	if math.Abs(estFreq-freq) > 5 {
		t.Fatalf("estimated frequency %v Hz, want ~%v", estFreq, freq)
	}
	if math.Abs(maxAmp-0.8) > 0.02 {
		t.Fatalf("mid-stream amplitude %v, want ~0.8 (edge energy jump?)", maxAmp)
	}
}

func TestDownsampleAntiAlias(t *testing.T) {
	// a tone above the output Nyquist must be strongly attenuated
	in := Format{SampleRate: 48000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	out := Format{SampleRate: 8000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	data := sineFloat32(48000, 20000, 48000, 1.0) // 20 kHz, way above 4 kHz Nyquist
	got := runPipeline(t, in, out, Options{Quality: QualityHigh}, data, 4096)
	var maxAmp float64
	for i := 2000; i < len(got)/4-2000; i++ { // skip edges
		v := math.Abs(float64(math.Float32frombits(binary.LittleEndian.Uint32(got[i*4:]))))
		if v > maxAmp {
			maxAmp = v
		}
	}
	if maxAmp > 0.01 {
		t.Fatalf("aliased tone leaks at amplitude %v, want < 0.01", maxAmp)
	}
}

func int16Bytes(vals ...int16) []byte {
	var b bytes.Buffer
	for _, v := range vals {
		binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

func TestBitDepthUpAndSameAreExact(t *testing.T) {
	// 8 -> 16: exact shift, no dither noise even when dither is enabled
	in := Format{SampleRate: 48000, BitDepth: 8, Channels: 1, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	data := []byte{0, 1, 127, 128, 255} // as int8: 0,1,127,-128,-1
	got := runPipeline(t, in, out, Options{Dither: DitherTPDF, DitherSeed: 5}, data, 3)
	// int8 127 -> 127/128*32768 = 32512
	want := int16Bytes(0, 256, 32512, -32768, -256)
	if !bytes.Equal(got, want) {
		t.Fatalf("8->16 not exact: got %v want %v", got, want)
	}

	// 16 -> 16 identity with dither enabled: bytes unchanged
	in16 := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	data16 := randBytes(11, 2000)
	got16 := runPipeline(t, in16, in16, Options{Dither: DitherTPDF, DitherSeed: 5}, data16, 100)
	if !bytes.Equal(got16, data16) {
		t.Fatalf("16->16 identity altered samples")
	}
}

func TestDitherReproducible(t *testing.T) {
	in := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 8, Channels: 1, SampleFormat: SampleFormatInt}
	data := randBytes(13, 4000)
	a := runPipeline(t, in, out, Options{Dither: DitherTPDF, DitherSeed: 99}, data, 512)
	b := runPipeline(t, in, out, Options{Dither: DitherTPDF, DitherSeed: 99}, data, 512)
	if !bytes.Equal(a, b) {
		t.Fatalf("same seed produced different dither")
	}
	c := runPipeline(t, in, out, Options{Dither: DitherTPDF, DitherSeed: 100}, data, 512)
	if bytes.Equal(a, c) {
		t.Fatalf("different seeds produced identical dither")
	}
	// dither must actually decorrelate: constant small signal should not
	// quantize to a constant
	var buf bytes.Buffer
	for i := 0; i < 2000; i++ {
		binary.Write(&buf, binary.LittleEndian, int16(40)) // 40/256 -> sub-LSB at 8 bit
	}
	d := runPipeline(t, in, out, Options{Dither: DitherTPDF, DitherSeed: 1}, buf.Bytes(), 4096)
	uniq := map[byte]bool{}
	for _, v := range d {
		uniq[v] = true
	}
	if len(uniq) < 2 {
		t.Fatalf("TPDF dither failed to toggle sub-LSB signal, output constant")
	}
}

func TestNoDitherRounds(t *testing.T) {
	in := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 8, Channels: 1, SampleFormat: SampleFormatInt}
	// 16-bit 1000 -> 1000/32768*128 = 3.90625 -> rounds to 4
	got := runPipeline(t, in, out, Options{Dither: DitherNone}, int16Bytes(1000, -1000, 32767, -32768), 8)
	want := []byte{4, 252, 127, 128} // int8: 4, -4, 127, -128
	if !bytes.Equal(got, want) {
		t.Fatalf("no-dither rounding: got %v want %v", got, want)
	}
}

func TestChannelMixing(t *testing.T) {
	mk := func(ch int) Format {
		return Format{SampleRate: 48000, BitDepth: 16, Channels: ch, SampleFormat: SampleFormatInt}
	}
	// stereo -> mono: average
	got := runPipeline(t, mk(2), mk(1), Options{}, int16Bytes(1000, 3000, -2000, 2000), 4)
	if want := int16Bytes(2000, 0); !bytes.Equal(got, want) {
		t.Fatalf("stereo->mono: got %v want %v", got, want)
	}
	// mono -> stereo: copy
	got = runPipeline(t, mk(1), mk(2), Options{}, int16Bytes(1234), 2)
	if want := int16Bytes(1234, 1234); !bytes.Equal(got, want) {
		t.Fatalf("mono->stereo: got %v want %v", got, want)
	}
	// stereo -> quad: copy ch0,ch1; ch2,ch3 silent
	got = runPipeline(t, mk(2), mk(4), Options{}, int16Bytes(100, 200), 4)
	if want := int16Bytes(100, 200, 0, 0); !bytes.Equal(got, want) {
		t.Fatalf("stereo->quad: got %v want %v", got, want)
	}
	// quad -> stereo: out0 = (in0+in2)/2, out1 = (in1+in3)/2
	got = runPipeline(t, mk(4), mk(2), Options{}, int16Bytes(1000, 2000, 3000, 4000), 8)
	if want := int16Bytes(2000, 3000); !bytes.Equal(got, want) {
		t.Fatalf("quad->stereo: got %v want %v", got, want)
	}
	// no crosstalk: signal only in left channel stays out of right
	var buf bytes.Buffer
	for i := 0; i < 100; i++ {
		binary.Write(&buf, binary.LittleEndian, int16(10000))
		binary.Write(&buf, binary.LittleEndian, int16(0))
	}
	got = runPipeline(t, mk(2), mk(2), Options{}, buf.Bytes(), 16)
	if !bytes.Equal(got, buf.Bytes()) {
		t.Fatalf("stereo identity crosstalk detected")
	}
}

func TestIntFloatConversion(t *testing.T) {
	i16 := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	f32 := Format{SampleRate: 48000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	got := runPipeline(t, i16, f32, Options{}, int16Bytes(32767, -32768, 0), 6)
	v0 := math.Float32frombits(binary.LittleEndian.Uint32(got[0:]))
	v1 := math.Float32frombits(binary.LittleEndian.Uint32(got[4:]))
	v2 := math.Float32frombits(binary.LittleEndian.Uint32(got[8:]))
	if math.Abs(float64(v0)-32767.0/32768.0) > 1e-7 || v1 != -1.0 || v2 != 0 {
		t.Fatalf("int->float: got %v %v %v", v0, v1, v2)
	}
	// float -> int
	var buf bytes.Buffer
	for _, v := range []float32{1.0, -1.0, 0.5} {
		binary.Write(&buf, binary.LittleEndian, v)
	}
	got = runPipeline(t, f32, i16, Options{}, buf.Bytes(), 12)
	if want := int16Bytes(32767, -32768, 16384); !bytes.Equal(got, want) {
		t.Fatalf("float->int: got %v want %v", got, want)
	}
}

func TestErrorKinds(t *testing.T) {
	f32 := Format{SampleRate: 48000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	i16 := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}

	// corrupt: NaN
	p, _ := NewPipeline(f32, i16, Options{})
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, float32(0.5))
	binary.Write(&buf, binary.LittleEndian, float32(math.NaN()))
	_, err := p.Write(buf.Bytes())
	pe, ok := err.(*Error)
	if !ok || pe.Kind != ErrKindCorrupt {
		t.Fatalf("NaN: got %v, want corrupt error", err)
	}
	if pe.Offset != 4 {
		t.Fatalf("NaN offset: got %d, want 4", pe.Offset)
	}

	// out of range: 1.5
	p, _ = NewPipeline(f32, i16, Options{})
	buf.Reset()
	binary.Write(&buf, binary.LittleEndian, float32(1.5))
	_, err = p.Write(buf.Bytes())
	if pe, ok = err.(*Error); !ok || pe.Kind != ErrKindOutOfRange {
		t.Fatalf("1.5: got %v, want out-of-range error", err)
	}

	// incomplete frame at Close
	p, _ = NewPipeline(i16, i16, Options{})
	if _, err = p.Write([]byte{1, 2, 3}); err != nil {
		t.Fatalf("partial write should be buffered, got %v", err)
	}
	err = p.Close()
	if pe, ok = err.(*Error); !ok || pe.Kind != ErrKindIncompleteFrame || pe.Offset != 2 {
		t.Fatalf("Close: got %v, want incomplete-frame at 2", err)
	}
	// supplying the missing byte must heal the pipeline
	if _, err = p.Write([]byte{4}); err != nil {
		t.Fatalf("healing write: %v", err)
	}
	if err = p.Close(); err != nil {
		t.Fatalf("Close after heal: %v", err)
	}

	// unsupported format
	_, err = NewPipeline(Format{SampleRate: 48000, BitDepth: 12, Channels: 1}, i16, Options{})
	if pe, ok = err.(*Error); !ok || pe.Kind != ErrKindUnsupported {
		t.Fatalf("12-bit: got %v, want unsupported error", err)
	}
}

func TestErrorDoesNotBreakPipeline(t *testing.T) {
	f32 := Format{SampleRate: 48000, BitDepth: 32, Channels: 1, SampleFormat: SampleFormatFloat}
	i16 := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	p1, _ := NewPipeline(f32, i16, Options{})
	p2, _ := NewPipeline(f32, i16, Options{})

	// p1 hits a corrupt sample
	var bad bytes.Buffer
	binary.Write(&bad, binary.LittleEndian, float32(math.Inf(1)))
	if _, err := p1.Write(bad.Bytes()); err == nil {
		t.Fatalf("expected corrupt error")
	}
	// p1 keeps working after the bad bytes are skipped
	var good bytes.Buffer
	binary.Write(&good, binary.LittleEndian, float32(0.5))
	if _, err := p1.Write(good.Bytes()); err != nil {
		t.Fatalf("p1 unusable after error: %v", err)
	}
	// p2 unaffected
	if _, err := p2.Write(good.Bytes()); err != nil {
		t.Fatalf("p2 affected by p1 error: %v", err)
	}
	p1.Close()
	p2.Close()
	o1, _ := io.ReadAll(reader{p1})
	o2, _ := io.ReadAll(reader{p2})
	if !bytes.Equal(o1, o2) || len(o1) != 2 {
		t.Fatalf("outputs differ or wrong length: %v vs %v", o1, o2)
	}
}

// reader adapts a drained pipeline to io.Reader for io.ReadAll.
type reader struct{ p *Pipeline }

func (r reader) Read(b []byte) (int, error) { return r.p.Read(b) }

func TestReadEOF(t *testing.T) {
	i16 := Format{SampleRate: 48000, BitDepth: 16, Channels: 1, SampleFormat: SampleFormatInt}
	p, _ := NewPipeline(i16, i16, Options{})
	p.Write(int16Bytes(1, 2))
	p.Close()
	buf := make([]byte, 64)
	n, err := p.Read(buf)
	if n != 4 || err != nil {
		t.Fatalf("first read: n=%d err=%v", n, err)
	}
	if _, err := p.Read(buf); err != io.EOF {
		t.Fatalf("second read: want EOF, got %v", err)
	}
}

func TestEmptyStream(t *testing.T) {
	in := Format{SampleRate: 44100, BitDepth: 16, Channels: 2, SampleFormat: SampleFormatInt}
	out := Format{SampleRate: 48000, BitDepth: 16, Channels: 2, SampleFormat: SampleFormatInt}
	got := runPipeline(t, in, out, Options{}, nil, 1)
	if len(got) != 0 {
		t.Fatalf("empty input produced %d bytes", len(got))
	}
}
