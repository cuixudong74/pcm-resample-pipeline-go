package pcm

import (
	"math"
	"testing"
)

func dcFrames(channels, n int, value float64) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, channels)
		for c := range out[i] {
			out[i][c] = value
		}
	}
	return out
}

func runResamplerChunks(inRate, outRate, channels, frames int, chunk int, method ResampleMethod, halfWidth int) [][]float64 {
	r := newMultiResampler(inRate, outRate, channels, ResampleConfig{
		Method: method, HalfWidth: halfWidth, Cutoff: 0,
	})
	var got [][]float64
	input := dcFrames(channels, frames, 0.25)
	for off := 0; off < len(input); off += chunk {
		end := off + chunk
		if end > len(input) {
			end = len(input)
		}
		out, err := r.push(input[off:end])
		if err != nil {
			panic(err)
		}
		got = append(got, out...)
	}
	tail, err := r.flush()
	if err != nil {
		panic(err)
	}
	got = append(got, tail...)
	return got
}

func assertFramesEqual(t *testing.T, got, want [][]float64, channels int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("frame count = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if len(got[i]) != channels || len(want[i]) != channels {
			t.Fatalf("frame %d channel count = %d/%d", i, len(got[i]), len(want[i]))
		}
		for c := range got[i] {
			if math.IsNaN(got[i][c]) || math.Abs(got[i][c]-want[i][c]) > 1e-12 {
				t.Fatalf("frame %d channel %d = %.12g, want %.12g", i, c, got[i][c], want[i][c])
			}
		}
	}
}

func TestResamplerChunkIndependenceAndLength(t *testing.T) {
	cases := []struct {
		name             string
		inRate, outRate  int
		frames, expected int
		method           ResampleMethod
		halfWidth        int
	}{
		{"linear-down", 48000, 44100, 100, 92, ResampleLinear, 0},
		{"linear-up", 8000, 16000, 100, 200, ResampleLinear, 0},
		{"sinc-down", 48000, 44100, 100, 92, ResampleSinc, 8},
		{"sinc-up", 8000, 16000, 100, 200, ResampleSinc, 8},
		{"odd-ratio", 44100, 48000, 73, 80, ResampleSinc, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reference := runResamplerChunks(tc.inRate, tc.outRate, 2, tc.frames, tc.frames, tc.method, tc.halfWidth)
			if len(reference) != tc.expected {
				t.Fatalf("length = %d, want %d", len(reference), tc.expected)
			}
			for _, chunk := range []int{1, 2, 3, 7, 17} {
				got := runResamplerChunks(tc.inRate, tc.outRate, 2, tc.frames, chunk, tc.method, tc.halfWidth)
				assertFramesEqual(t, got, reference, 2)
			}
			// DC must be preserved away from both deterministic zero-extended
			// edges. Linear interpolation needs one input sample of margin.
			margin := tc.halfWidth * 2
			if tc.method == ResampleLinear {
				margin = 2
			}
			for i := margin; i < len(reference)-margin; i++ {
				for c := 0; c < 2; c++ {
					if math.Abs(reference[i][c]-0.25) > 1e-10 {
						t.Fatalf("DC at frame %d ch %d = %.10f", i, c, reference[i][c])
					}
				}
			}
		})
	}
}

func TestEqualRatePassthroughDoesNotBufferTail(t *testing.T) {
	r := newMultiResampler(12345, 12345, 1, ResampleConfig{Method: ResampleSinc, HalfWidth: 8})
	in := dcFrames(1, 3, 0.5)
	got, err := r.push(in)
	if err != nil {
		t.Fatal(err)
	}
	assertFramesEqual(t, got, in, 1)
	tail, err := r.flush()
	if err != nil {
		t.Fatal(err)
	}
	if tail != nil {
		t.Fatalf("equal-rate flush returned %d frames", len(tail))
	}
}
