package pcm

import "fmt"

// DitherMethod selects quantization noise behavior.
type DitherMethod uint8

const (
	// DitherNone performs deterministic rounding without added noise.
	DitherNone DitherMethod = iota
	// DitherTPDF adds triangular-probability dither immediately before integer
	// quantization. It is valid only when reducing integer precision.
	DitherTPDF
)

// DitherConfig configures deterministic requantization noise.
//
// The same Seed always produces the same stream. Dither samples are applied in
// interleaved output order and are independent of the chunk boundary.
type DitherConfig struct {
	Method DitherMethod
	Seed   uint64
}

// ditherGenerator is a deterministic 64-bit PCG generator.
type ditherGenerator struct {
	state uint64
	inc   uint64
}

func newDitherGenerator(seed uint64) *ditherGenerator {
	// PCG initialization; even a zero user seed produces a non-zero stream.
	g := &ditherGenerator{state: 0, inc: 0x9e3779b97f4a7c15 | 1}
	g.state = g.state*6364136223846793005 + g.inc
	g.state += seed
	g.state = g.state*6364136223846793005 + g.inc
	return g
}

// next returns one TPDF value in [-1,+1), measured in target integer LSBs.
// Two independent uniform values are subtracted, giving a triangular PDF.
func (g *ditherGenerator) next() float64 {
	a := g.uniformUnit()
	b := g.uniformUnit()
	return a - b
}

func (g *ditherGenerator) uniformUnit() float64 {
	// 53 random bits provide exact granularity well below all supported LSBs.
	g.state = g.state*6364136223846793005 + g.inc
	xorshifted := uint64(((g.state >> 18) ^ g.state) >> 27)
	rot := g.state >> 59
	bits := (xorshifted >> rot) | (xorshifted << ((-rot) & 31))
	bits >>= 64 - 53
	return float64(bits) / float64(uint64(1)<<53)
}

func validateDither(in, out PCMFormat, cfg DitherConfig) error {
	switch cfg.Method {
	case DitherNone:
		return nil
	case DitherTPDF:
		if out.SampleFormat.floating() {
			return newStreamError(ErrorInvalidConfig, 0, -1,
				"dither is not allowed when encoding floating-point samples")
		}
		if !reducesIntegerPrecision(in, out) {
			return newStreamError(ErrorInvalidConfig, 0, -1,
				"TPDF dither is valid only when reducing integer bit depth")
		}
		return nil
	default:
		return newStreamError(ErrorInvalidConfig, 0, -1,
			fmt.Sprintf("unsupported dither method: %d", cfg.Method))
	}
}

func shouldDither(in, out PCMFormat, method DitherMethod) bool {
	return method == DitherTPDF && reducesIntegerPrecision(in, out)
}

// reducesIntegerPrecision reports whether the output integer code has fewer
// representable levels than the input. Float inputs have more dynamic levels
// than every supported integer code. Up-conversion and equal precision never
// dither.
func reducesIntegerPrecision(in, out PCMFormat) bool {
	if out.SampleFormat.floating() {
		return false
	}
	if in.SampleFormat.floating() {
		return true
	}
	return in.SampleFormat.bits() > out.SampleFormat.bits()
}
