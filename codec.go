package pcm

import (
	"encoding/binary"
	"math"
)

// decodeSamples decodes one or more complete interleaved frames.
// baseOffset is the byte offset of p[0] in the logical stream.
func decodeSamples(p []byte, f PCMFormat, baseOffset int64, baseFrame int64) ([]float64, error) {
	bytesPerSample := f.SampleFormat.bytesPerSample()
	if len(p)%bytesPerSample != 0 {
		return nil, newStreamError(ErrorIncompleteFrame, baseOffset+int64(len(p)),
			int64(len(p)/bytesPerSample/f.Channels), "data ends in the middle of a sample")
	}
	if len(p)%f.frameBytes() != 0 {
		return nil, newStreamError(ErrorIncompleteFrame, baseOffset+int64(len(p)),
			int64(len(p)/f.frameBytes()), "data ends in the middle of an interleaved frame")
	}

	out := make([]float64, len(p)/bytesPerSample)
	big := f.SampleFormat.bigEndian()
	for i := range out {
		off := i * bytesPerSample
		absOffset := baseOffset + int64(off)
		frame := baseFrame + int64(i/f.Channels)

		var v float64
		switch f.SampleFormat {
		case FormatUint8:
			v = (float64(p[off]) - 128.0) / 128.0
		case FormatInt16LE, FormatInt16BE:
			var u uint16
			if big {
				u = binary.BigEndian.Uint16(p[off:])
			} else {
				u = binary.LittleEndian.Uint16(p[off:])
			}
			n := int16(u)
			if n >= 0 {
				v = float64(n) / 32767.0
			} else {
				v = float64(n) / 32768.0
			}
		case FormatInt24LE, FormatInt24BE:
			var u uint32
			if big {
				u = uint32(p[off])<<16 | uint32(p[off+1])<<8 | uint32(p[off+2])
			} else {
				u = uint32(p[off]) | uint32(p[off+1])<<8 | uint32(p[off+2])<<16
			}
			if u&0x800000 != 0 {
				u |= 0xff000000
			}
			n := int32(u)
			if n >= 0 {
				v = float64(n) / 8388607.0
			} else {
				v = float64(n) / 8388608.0
			}
		case FormatInt32LE, FormatInt32BE:
			var u uint32
			if big {
				u = binary.BigEndian.Uint32(p[off:])
			} else {
				u = binary.LittleEndian.Uint32(p[off:])
			}
			n := int32(u)
			if n >= 0 {
				v = float64(n) / 2147483647.0
			} else {
				v = float64(n) / 2147483648.0
			}
		case FormatFloat32LE, FormatFloat32BE:
			var u uint32
			if big {
				u = binary.BigEndian.Uint32(p[off:])
			} else {
				u = binary.LittleEndian.Uint32(p[off:])
			}
			v = float64(math.Float32frombits(u))
		case FormatFloat64LE, FormatFloat64BE:
			var u uint64
			if big {
				u = binary.BigEndian.Uint64(p[off:])
			} else {
				u = binary.LittleEndian.Uint64(p[off:])
			}
			v = math.Float64frombits(u)
		default:
			return nil, newStreamError(ErrorUnsupportedFormat, absOffset, frame, "unsupported sample format")
		}

		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, newStreamError(ErrorCorruptData, absOffset, frame,
				"float sample is NaN or infinite")
		}
		if v < -1.0 || v > 1.0 {
			return nil, newStreamError(ErrorValueOutOfRange, absOffset, frame,
				"finite float sample is outside [-1,+1]")
		}
		out[i] = v
	}
	return out, nil
}

// encodeSamples encodes one or more complete interleaved frames.
func encodeSamples(p []float64, f PCMFormat, useDither bool, d *ditherGenerator) ([]byte, error) {
	bytesPerSample := f.SampleFormat.bytesPerSample()
	if len(p)%f.Channels != 0 {
		return nil, newStreamError(ErrorIncompleteFrame, 0, int64(len(p)/f.Channels),
			"internal sample buffer ends in the middle of a frame")
	}
	out := make([]byte, len(p)*bytesPerSample)
	big := f.SampleFormat.bigEndian()
	for i, v := range p {
		off := i * bytesPerSample
		frame := int64(i / f.Channels)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, newStreamError(ErrorCorruptData, int64(off), frame,
				"sample cannot be encoded as NaN or infinity")
		}

		switch f.SampleFormat {
		case FormatUint8:
			q := quantizeInteger(v, 8, useDither, d) + 128
			out[off] = uint8(q)
		case FormatInt16LE, FormatInt16BE:
			q := uint16(quantizeInteger(v, 16, useDither, d))
			if big {
				binary.BigEndian.PutUint16(out[off:], q)
			} else {
				binary.LittleEndian.PutUint16(out[off:], q)
			}
		case FormatInt24LE, FormatInt24BE:
			q := uint32(quantizeInteger(v, 24, useDither, d))
			if big {
				out[off] = byte(q >> 16)
				out[off+1] = byte(q >> 8)
				out[off+2] = byte(q)
			} else {
				out[off] = byte(q)
				out[off+1] = byte(q >> 8)
				out[off+2] = byte(q >> 16)
			}
		case FormatInt32LE, FormatInt32BE:
			q := uint32(quantizeInteger(v, 32, useDither, d))
			if big {
				binary.BigEndian.PutUint32(out[off:], q)
			} else {
				binary.LittleEndian.PutUint32(out[off:], q)
			}
		case FormatFloat32LE, FormatFloat32BE:
			// Finite filter overshoot is representable; only non-finite values
			// have no defined PCM meaning.
			bits := math.Float32bits(float32(v))
			if big {
				binary.BigEndian.PutUint32(out[off:], bits)
			} else {
				binary.LittleEndian.PutUint32(out[off:], bits)
			}
		case FormatFloat64LE, FormatFloat64BE:
			// Finite filter overshoot is representable; only non-finite values
			// have no defined PCM meaning.
			bits := math.Float64bits(v)
			if big {
				binary.BigEndian.PutUint64(out[off:], bits)
			} else {
				binary.LittleEndian.PutUint64(out[off:], bits)
			}
		default:
			return nil, newStreamError(ErrorUnsupportedFormat, int64(off), frame, "unsupported sample format")
		}
	}
	return out, nil
}

func quantizeInteger(v float64, bits int, useDither bool, d *ditherGenerator) int64 {
	if v > 1.0 {
		v = 1.0
	}
	if v < -1.0 {
		v = -1.0
	}
	maxPositive := int64(1)<<(bits-1) - 1
	minMagnitude := float64(int64(1) << (bits - 1))
	q := v * minMagnitude
	if useDither && d != nil {
		q += d.next()
	}
	n := int64(math.Round(q))
	if n > maxPositive {
		n = maxPositive
	}
	if n < -int64(minMagnitude) {
		n = -int64(minMagnitude)
	}
	return n
}
