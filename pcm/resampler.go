package pcm

import "math"

// Quality selects the resampling filter size, trading quality against latency.
type Quality int

const (
	// QualityLow uses a short filter (8 taps per side, lowest latency).
	QualityLow Quality = iota
	// QualityMedium is the default balance (16 taps per side).
	QualityMedium
	// QualityHigh uses a long filter (32 taps per side, best quality).
	QualityHigh
)

func halfTapsFor(q Quality) int {
	switch q {
	case QualityLow:
		return 8
	case QualityHigh:
		return 32
	default:
		return 16
	}
}

// resampler is a streaming windowed-sinc resampler.
//
// Algorithm: each output frame n is placed at input time t = n*inRate/outRate
// and computed as a Blackman-windowed sinc interpolation over the input
// samples within `radius` of t. The anti-aliasing cutoff is
// fc = min(1, outRate/inRate) (Nyquist = 1), so the support radius in input
// samples is halfTaps/fc.
//
// Boundary behavior (deterministic): samples outside the received stream
// are treated as zero, and the kernel is renormalized by the sum of the
// weights of the available samples, so a DC signal passes with unity gain
// right up to both edges — no energy jump or fade at the seams.
//
// The total number of output frames for a finalized stream of N input
// frames is round(N*outRate/inRate); no input sample is dropped or
// duplicated. Output depends only on the sample sequence, never on how
// the input was chunked.
type resampler struct {
	inRate, outRate int
	channels        int
	halfTaps        int
	fc              float64 // normalized cutoff
	radius          float64 // kernel support radius in input frames
	step            float64 // input frames per output frame

	buf      []float64 // interleaved history, frame `base` at buf[0]
	base     int64
	inCount  int64 // input frames received so far
	outCount int64 // output frames produced so far
	final    bool
}

func newResampler(inRate, outRate, channels int, q Quality) *resampler {
	ht := halfTapsFor(q)
	fc := float64(outRate) / float64(inRate)
	if fc > 1 {
		fc = 1
	}
	return &resampler{
		inRate:   inRate,
		outRate:  outRate,
		channels: channels,
		halfTaps: ht,
		fc:       fc,
		radius:   float64(ht) / fc,
		step:     float64(inRate) / float64(outRate),
	}
}

// Latency returns the filter group delay in input frames.
func (r *resampler) Latency() float64 { return r.radius }

// TotalOutputFrames returns the exact number of output frames a finalized
// stream of n input frames produces.
func (r *resampler) TotalOutputFrames(n int64) int64 {
	return (n*int64(r.outRate) + int64(r.inRate)/2) / int64(r.inRate)
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	x *= math.Pi
	return math.Sin(x) / x
}

func blackman(u float64) float64 {
	if u <= -1 || u >= 1 {
		return 0
	}
	return 0.42 + 0.5*math.Cos(math.Pi*u) + 0.08*math.Cos(2*math.Pi*u)
}

// process consumes interleaved input frames and appends produced output
// frames to out. final must be true on the last call to flush the tail.
func (r *resampler) process(in []float64, out []float64, final bool) []float64 {
	if len(in) > 0 {
		r.buf = append(r.buf, in...)
		r.inCount += int64(len(in) / r.channels)
	}
	if final {
		r.final = true
	}
	for r.canEmit() {
		out = r.emit(out)
	}
	r.discard()
	return out
}

// canEmit reports whether the next output frame can be computed.
func (r *resampler) canEmit() bool {
	if r.final {
		return r.outCount < r.TotalOutputFrames(r.inCount)
	}
	t := float64(r.outCount) * r.step
	// need every sample k with t-radius <= k <= t+radius to be available
	// (k < 0 is zero-padded, so only the upper edge gates emission)
	return t+r.radius <= float64(r.inCount-1)
}

// emit computes one output frame and appends it to out.
func (r *resampler) emit(out []float64) []float64 {
	t := float64(r.outCount) * r.step
	k0 := int64(math.Ceil(t - r.radius))
	k1 := int64(math.Floor(t + r.radius))
	if k0 < 0 {
		k0 = 0
	}
	if k1 > r.inCount-1 {
		k1 = r.inCount - 1
	}
	for ch := 0; ch < r.channels; ch++ {
		var sum, wsum float64
		for k := k0; k <= k1; k++ {
			x := t - float64(k)
			w := r.fc * sinc(r.fc*x) * blackman(x/r.radius)
			sum += r.buf[(k-r.base)*int64(r.channels)+int64(ch)] * w
			wsum += w
		}
		if wsum != 0 {
			sum /= wsum
		}
		out = append(out, sum)
	}
	r.outCount++
	return out
}

// discard drops history frames no future output can reference.
func (r *resampler) discard() {
	t := float64(r.outCount) * r.step
	minNeeded := int64(math.Ceil(t - r.radius))
	if minNeeded < 0 {
		minNeeded = 0
	}
	if drop := minNeeded - r.base; drop > 0 {
		n := int(drop) * r.channels
		if n > len(r.buf) {
			n = len(r.buf)
		}
		copy(r.buf, r.buf[n:])
		r.buf = r.buf[:len(r.buf)-n]
		r.base += int64(n / r.channels)
	}
}
