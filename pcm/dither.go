package pcm

// DitherMode selects the noise shaping used when reducing bit depth.
type DitherMode int

const (
	// DitherNone applies no dither (plain rounding).
	DitherNone DitherMode = iota
	// DitherTPDF applies triangular-PDF dither spanning 2 LSB peak-to-peak
	// (the difference of two independent uniform variables in [0,1) LSB),
	// which fully decorrelates quantization error from the signal.
	DitherTPDF
)

// ditherer generates deterministic dither noise from a seeded PRNG
// (splitmix64). The noise sequence depends only on the seed and the
// number of samples produced, never on how input was chunked.
type ditherer struct {
	state uint64
	mode  DitherMode
}

func newDitherer(mode DitherMode, seed uint64) *ditherer {
	return &ditherer{state: seed, mode: mode}
}

// next advances the splitmix64 generator and returns a uniform value in [0,1).
func (d *ditherer) next() float64 {
	d.state += 0x9E3779B97F4A7C15
	z := d.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z = z ^ (z >> 31)
	return float64(z>>11) * (1.0 / (1 << 53))
}

// noise returns one dither sample in units of 1 LSB of the target depth.
// For DitherNone it returns 0 and does not advance the generator.
func (d *ditherer) noise() float64 {
	if d.mode == DitherTPDF {
		return d.next() - d.next()
	}
	return 0
}
