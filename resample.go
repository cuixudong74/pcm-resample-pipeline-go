package pcm

import (
	"fmt"
	"math"
)

// ResampleMethod selects the interpolation/anti-imaging filter.
type ResampleMethod uint8

const (
	// ResampleLinear uses two-tap linear interpolation. It has one input-frame
	// algorithmic delay and does not apply an anti-aliasing filter, so it is
	// intended only for low-quality or rate ratios where aliasing is tolerable.
	ResampleLinear ResampleMethod = iota
	// ResampleSinc uses a Hann-windowed sinc FIR with configurable half width.
	// It low-pass-filters when downsampling.
	ResampleSinc
)

// ResampleConfig controls quality, latency and the deterministic filter edge
// rule.
//
// HalfWidth is the filter support measured in input frames on each side. The
// normal streaming delay is therefore approximately HalfWidth input frames.
// Cutoff is a fraction in (0,1], relative to the usable Nyquist frequency;
// zero selects min(1, inputRate/outputRate).
//
// Both stream boundaries are deterministic zero-extension boundaries. Samples
// before the first and after the last real input are treated as zero. Exactly
// ceil(inputFrames*outputRate/inputRate) output frames are produced, so no
// final input sample is dropped or duplicated.
type ResampleConfig struct {
	Method    ResampleMethod
	HalfWidth int
	Cutoff    float64
}

func defaultResampleConfig() ResampleConfig {
	return ResampleConfig{Method: ResampleSinc, HalfWidth: 32, Cutoff: 0}
}

func validateResample(cfg ResampleConfig) error {
	switch cfg.Method {
	case ResampleLinear:
		if cfg.HalfWidth < 0 || cfg.HalfWidth > 1024 {
			return newStreamError(ErrorInvalidConfig, 0, -1, "linear resampling half width must be in [0,1024]")
		}
		if cfg.Cutoff < 0 || cfg.Cutoff > 1 || math.IsNaN(cfg.Cutoff) {
			return newStreamError(ErrorInvalidConfig, 0, -1, "resampling cutoff must be in [0,1]")
		}
	case ResampleSinc:
		if cfg.HalfWidth == 0 {
			cfg.HalfWidth = 32
		}
		if cfg.HalfWidth < 1 || cfg.HalfWidth > 1024 {
			return newStreamError(ErrorInvalidConfig, 0, -1, "sinc half width must be in [1,1024]")
		}
		if cfg.Cutoff < 0 || cfg.Cutoff > 1 || math.IsNaN(cfg.Cutoff) {
			return newStreamError(ErrorInvalidConfig, 0, -1, "resampling cutoff must be in [0,1]")
		}
	default:
		return newStreamError(ErrorInvalidConfig, 0, -1,
			fmt.Sprintf("unsupported resampling method: %d", cfg.Method))
	}
	return nil
}

type channelResampler struct {
	inRate  int
	outRate int
	cfg     ResampleConfig
	cutoff  float64
	widthT  int64

	stepIn  int64
	stepOut int64

	samples  []float64
	base     int64
	next     int64
	overflow bool
}

func newChannelResampler(inRate, outRate int, cfg ResampleConfig) *channelResampler {
	g := gcd64(int64(inRate), int64(outRate))
	cutoff := cfg.Cutoff
	if cutoff == 0 {
		cutoff = math.Min(1.0, float64(inRate)/float64(outRate))
	}
	t := 1
	if cfg.Method == ResampleSinc {
		t = cfg.HalfWidth
		if t == 0 {
			t = 32
		}
	}
	return &channelResampler{
		inRate:  inRate,
		outRate: outRate,
		cfg:     cfg,
		cutoff:  cutoff,
		widthT:  int64(t),
		stepIn:  int64(inRate) / g,
		stepOut: int64(outRate) / g,
	}
}

func (r *channelResampler) push(v float64) []float64 {
	r.samples = append(r.samples, v)
	return r.drain(false)
}

func (r *channelResampler) flush() []float64 {
	return r.drain(true)
}

func (r *channelResampler) inputLength() int64 {
	return r.base + int64(len(r.samples))
}

// maxResampledFrames bounds stream duration so exact integer phase arithmetic
// cannot overflow on pathological multi-hundred-million-frame inputs. At the
// supported maximum sample rate of 1,000,000 Hz it still allows about 9.2
// trillion samples (roughly 107 days); at ordinary rates it is far longer.
const maxResampledFrames int64 = 1 << 33

func (r *channelResampler) drain(end bool) []float64 {
	n := r.inputLength()
	var out []float64
	for {
		m := r.next
		if m > maxResampledFrames || n > maxResampledFrames {
			r.overflow = true
			break
		}
		numerator := m * r.stepIn
		if !(numerator < n*r.stepOut) {
			break
		}

		floorX, maxJ := r.support(m, numerator)
		if !end && maxJ >= n {
			break
		}

		var y float64
		if r.cfg.Method == ResampleLinear {
			y = r.linearAt(floorX, numerator)
		} else {
			y = r.sincAt(float64(numerator) / float64(r.stepOut))
		}
		out = append(out, y)
		r.next++
	}

	if !end {
		// next is the smallest not-yet-emitted output index, so all future
		// centers are at or after its input position. Drop only samples that
		// lie completely to the left of that kernel support.
		nextFloor := floorDivPositiveDen(r.next*r.stepIn, r.stepOut)
		minJ := nextFloor - r.widthT
		if minJ > r.base && minJ <= n {
			drop := int(minJ - r.base)
			r.samples = append([]float64(nil), r.samples[drop:]...)
			r.base = minJ
		}
	}
	return out
}

// support returns floor(output center input index) and the highest finite
// input index needed by the right side of the kernel.
func (r *channelResampler) support(m, numerator int64) (int64, int64) {
	floorX := floorDivPositiveDen(numerator, r.stepOut)
	return floorX, floorX + r.widthT
}

func (r *channelResampler) linearAt(floorX, numerator int64) float64 {
	rem := numerator - floorX*r.stepOut
	frac := float64(rem) / float64(r.stepOut)
	return r.at(floorX)*(1.0-frac) + r.at(floorX+1)*frac
}

func (r *channelResampler) sincAt(x float64) float64 {
	minJ := int64(math.Ceil(x - float64(r.widthT)))
	maxJ := int64(math.Floor(x + float64(r.widthT)))
	coeffs := make([]float64, 0, maxJ-minJ+1)
	values := make([]float64, 0, maxJ-minJ+1)
	sum := 0.0
	for j := minJ; j <= maxJ; j++ {
		d := float64(j) - x
		window := 0.5 + 0.5*math.Cos(math.Pi*d/float64(r.widthT))
		var h float64
		if d == 0 {
			h = r.cutoff
		} else {
			z := math.Pi * d * r.cutoff
			h = math.Sin(z) / z * window
		}
		coeffs = append(coeffs, h)
		values = append(values, r.at(j))
		sum += h
	}
	if sum == 0 {
		return 0
	}
	y := 0.0
	for i := range coeffs {
		y += coeffs[i] / sum * values[i]
	}
	return y
}

func (r *channelResampler) at(j int64) float64 {
	if j < r.base || j >= r.base+int64(len(r.samples)) {
		return 0
	}
	return r.samples[j-r.base]
}

type multiResampler struct {
	channels    []*channelResampler
	passthrough bool
	count       int
}

func newMultiResampler(inRate, outRate, channels int, cfg ResampleConfig) *multiResampler {
	if inRate == outRate {
		return &multiResampler{passthrough: true, count: channels}
	}
	rs := &multiResampler{count: channels}
	for i := 0; i < channels; i++ {
		rs.channels = append(rs.channels, newChannelResampler(inRate, outRate, cfg))
	}
	return rs
}

func (r *multiResampler) push(frames [][]float64) ([][]float64, error) {
	if r.passthrough {
		return frames, nil
	}
	if len(frames) == 0 || r.count == 0 {
		return nil, r.overflowError()
	}
	perChannel := make([][]float64, r.count)
	for c := 0; c < r.count; c++ {
		perChannel[c] = make([]float64, len(frames))
	}
	for i, frame := range frames {
		for c := 0; c < r.count; c++ {
			perChannel[c][i] = frame[c]
		}
	}
	drained := make([][]float64, r.count)
	minLen := 0
	for c := 0; c < r.count; c++ {
		for _, v := range perChannel[c] {
			drained[c] = append(drained[c], r.channels[c].push(v)...)
		}
		if c == 0 || len(drained[c]) < minLen {
			minLen = len(drained[c])
		}
	}
	return assembleFrames(drained, minLen), r.overflowError()
}

func (r *multiResampler) flush() ([][]float64, error) {
	if r.passthrough || r.count == 0 {
		return nil, nil
	}
	drained := make([][]float64, r.count)
	minLen := 0
	for c := 0; c < r.count; c++ {
		drained[c] = r.channels[c].flush()
		if c == 0 || len(drained[c]) < minLen {
			minLen = len(drained[c])
		}
	}
	return assembleFrames(drained, minLen), r.overflowError()
}

func (r *multiResampler) overflowError() error {
	for _, ch := range r.channels {
		if ch.overflow {
			return newStreamError(ErrorInvalidConfig, 0, -1,
				"stream exceeds the maximum supported resampler duration")
		}
	}
	return nil
}

func assembleFrames(perChannel [][]float64, frames int) [][]float64 {
	if frames == 0 || len(perChannel) == 0 {
		return nil
	}
	out := make([][]float64, frames)
	for i := range out {
		out[i] = make([]float64, len(perChannel))
		for c := range perChannel {
			out[i][c] = perChannel[c][i]
		}
	}
	return out
}

func floorDivPositiveDen(a, b int64) int64 {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

func gcd64(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}
