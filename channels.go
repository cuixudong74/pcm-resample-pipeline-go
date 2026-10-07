package pcm

import "math"

// channelConverter converts one deinterleaved/interleaved frame using a fixed
// row-major matrix. Matrix rows are outputs and columns are inputs.
type channelConverter struct {
	inChannels  int
	outChannels int
	matrix      [][]float64
}

func newChannelConverter(in, out PCMFormat) (*channelConverter, error) {
	matrix := buildChannelMatrix(in.Layout, out.Layout)
	if len(matrix) != out.Channels || len(matrix[0]) != in.Channels {
		return nil, newStreamError(ErrorInvalidConfig, 0, -1, "internal channel matrix has the wrong shape")
	}
	return &channelConverter{
		inChannels:  in.Channels,
		outChannels: out.Channels,
		matrix:      matrix,
	}, nil
}

func (c *channelConverter) convertFrame(src []float64, dst []float64) int {
	if len(src) < c.inChannels || cap(dst) < c.outChannels {
		return 0
	}
	dst = dst[:c.outChannels]
	for o := 0; o < c.outChannels; o++ {
		row := c.matrix[o]
		var sum float64
		for i := 0; i < c.inChannels; i++ {
			sum += row[i] * src[i]
		}
		dst[o] = sum
	}
	return c.outChannels
}

func buildChannelMatrix(in, out ChannelLayout) [][]float64 {
	m := make([][]float64, out.Count())
	for i := range m {
		m[i] = make([]float64, in.Count())
	}

	// Equal-power mono-to-stereo phantom-center rule.
	if in.Count() == 1 && out.Count() == 2 {
		v := 1.0 / math.Sqrt2
		m[indexOf(out, ChannelFrontLeft)][0] = v
		m[indexOf(out, ChannelFrontRight)][0] = v
		return m
	}

	sqrtHalf := 1.0 / math.Sqrt2
	for oi, och := range out.channels {
		weights := make([]float64, in.Count())
		active := 0

		switch {
		case out.Count() == 1:
			// Mono downmix includes all non-LFE channels equally and excludes
			// the LFE bandwidth-limited effects channel.
			for ii, ich := range in.channels {
				if ich != ChannelLowFrequency {
					weights[ii] = 1
					active++
				}
			}
		case out.Count() == 2 && (in.Count() == 6 || in.Count() == 8) &&
			(och == ChannelFrontLeft || och == ChannelFrontRight):
			side := ChannelFrontLeft
			back := ChannelBackLeft
			sideSurround := ChannelSideLeft
			if och == ChannelFrontRight {
				side = ChannelFrontRight
				back = ChannelBackRight
				sideSurround = ChannelSideRight
			}
			addMixWeight(weights, in, side, 1)
			addMixWeight(weights, in, ChannelFrontCenter, sqrtHalf)
			addMixWeight(weights, in, back, sqrtHalf)
			addMixWeight(weights, in, sideSurround, sqrtHalf)
			active = countNonZero(weights)
		case out.Count() == 6 && in.Count() == 8 &&
			(och == ChannelBackLeft || och == ChannelBackRight):
			surround := ChannelSideLeft
			if och == ChannelBackRight {
				surround = ChannelSideRight
			}
			addMixWeight(weights, in, och, 1)
			addMixWeight(weights, in, surround, 1)
			active = countNonZero(weights)
		default:
			if ii := indexOf(in, och); ii >= 0 {
				weights[ii] = 1
				active = 1
			}
		}

		if active > 0 {
			// Normalize each destination row so arbitrary full-scale samples
			// cannot add to more than +/-1. This is a deterministic linear
			// downmix, not clipping or automatic gain control.
			denominator := 0.0
			for _, w := range weights {
				denominator += math.Abs(w)
			}
			if denominator == 0 {
				denominator = 1
			}
			for ii := range weights {
				m[oi][ii] = weights[ii] / denominator
			}
		}
	}
	return m
}

func addMixWeight(dst []float64, layout ChannelLayout, ch Channel, weight float64) {
	if i := indexOf(layout, ch); i >= 0 {
		dst[i] = weight
	}
}

func indexOf(layout ChannelLayout, ch Channel) int {
	for i, candidate := range layout.channels {
		if candidate == ch {
			return i
		}
	}
	return -1
}

func countNonZero(values []float64) int {
	n := 0
	for _, v := range values {
		if v != 0 {
			n++
		}
	}
	return n
}
