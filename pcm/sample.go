package pcm

import (
	"fmt"
	"math"
)

// decodeSample converts one raw sample (little-endian) to normalized
// float64 in [-1, 1]. Integer depth N maps linearly with full scale
// 2^(N-1). Float samples must be finite and within [-1, 1].
func decodeSample(f Format, b []byte) (float64, error) {
	if f.SampleFormat == SampleFormatFloat {
		var v float64
		if f.BitDepth == 32 {
			v = float64(math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
		} else {
			v = math.Float64frombits(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
				uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, &Error{Kind: ErrKindCorrupt, Offset: -1, Msg: "non-finite float sample (NaN/Inf)"}
		}
		if v < -1.0 || v > 1.0 {
			return 0, &Error{Kind: ErrKindOutOfRange, Offset: -1,
				Msg: fmt.Sprintf("float sample %v outside [-1, 1]", v)}
		}
		return v, nil
	}
	// signed integer, little-endian, sign-extended
	n := f.BytesPerSample()
	var u uint64
	for i := 0; i < n; i++ {
		u |= uint64(b[i]) << (8 * i)
	}
	shift := uint(64 - 8*n)
	s := int64(u<<shift) >> shift
	return float64(s) / float64(int64(1)<<(f.BitDepth-1)), nil
}

// encodeSample converts a normalized float64 back to raw little-endian
// bytes. Values are clamped to the representable range. When d is non-nil
// (integer output only), one dither sample in LSB units is added before
// rounding; the caller only passes a ditherer when reducing bit depth.
func encodeSample(f Format, v float64, d *ditherer, dst []byte) error {
	if f.SampleFormat == SampleFormatFloat {
		v = math.Max(-1.0, math.Min(1.0, v))
		if f.BitDepth == 32 {
			bits := math.Float32bits(float32(v))
			dst[0] = byte(bits)
			dst[1] = byte(bits >> 8)
			dst[2] = byte(bits >> 16)
			dst[3] = byte(bits >> 24)
		} else {
			bits := math.Float64bits(v)
			for i := 0; i < 8; i++ {
				dst[i] = byte(bits >> (8 * i))
			}
		}
		return nil
	}
	full := float64(int64(1) << (f.BitDepth - 1))
	x := v * full
	if d != nil {
		x += d.noise()
	}
	x = math.Round(x)
	max := full - 1
	min := -full
	if x > max {
		x = max
	} else if x < min {
		x = min
	}
	s := int64(x)
	n := f.BytesPerSample()
	u := uint64(s)
	for i := 0; i < n; i++ {
		dst[i] = byte(u >> (8 * i))
	}
	return nil
}
