package pcm

import "testing"

func TestResampleHighRatioSmallInputTrim(t *testing.T) {
	rates := [][2]int{{100, 192000}, {1, 100000}, {1000, 1000000}}
	for _, rr := range rates {
		r := newMultiResampler(rr[0], rr[1], 1, ResampleConfig{Method: ResampleSinc, HalfWidth: 4})
		var got [][]float64
		for _, v := range []float64{0, 0.5, -0.25, 0.75} {
			out, err := r.push([][]float64{{v}})
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, out...)
		}
		tail, err := r.flush()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, tail...)
		want := (4*rr[1] + rr[0] - 1) / rr[0]
		if len(got) != want {
			t.Fatalf("%v:%v frames = %d, want %d", rr[0], rr[1], len(got), want)
		}
	}
}
