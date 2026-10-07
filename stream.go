package pcm

// Stream converts one byte-oriented PCM stream. Each stream is independent and
// retains only the state needed to bridge arbitrary Append boundaries.
type Stream struct {
	converter *Converter
	channels  *channelConverter
	resampler *multiResampler
	dither    *ditherGenerator

	pending     []byte
	inputOffset int64
	converted   []float64
	closed      bool
	failed      bool
}

// Append accepts a chunk of any non-negative size. It returns converted bytes
// as soon as enough input is available. Output chunks are not aligned with
// input chunks, but concatenating every returned slice produces the same total
// result for every chunking strategy.
//
// If an error is returned, all complete input frames processed before the
// failure remain represented in the returned output. A failed stream cannot be
// used again; create a new stream to retry independently.
func (s *Stream) Append(p []byte) ([]byte, error) {
	if s.closed {
		return nil, newStreamError(ErrorStreamClosed, s.inputOffset, -1, "Append called after Close")
	}
	if s.failed {
		return nil, newStreamError(ErrorStreamClosed, s.inputOffset, -1, "stream has already failed")
	}

	in := s.converter.in
	out := s.converter.out
	frameBytes := in.frameBytes()
	if len(p) > 0 {
		// Copy because callers are allowed to reuse p after Append returns.
		s.pending = append(s.pending, p...)
	}

	consumable := len(s.pending) - len(s.pending)%frameBytes
	var result []byte

	// Decode and advance one input frame at a time. If a batch contains valid
	// frames followed by a corrupt frame, the resampler and returned output for
	// the valid prefix must not be rolled back.
	for offset := 0; offset < consumable; offset += frameBytes {
		samples, err := decodeSamples(s.pending[offset:offset+frameBytes], in,
			s.inputOffset+int64(offset), (s.inputOffset+int64(offset))/int64(frameBytes))
		if err != nil {
			s.inputOffset += int64(offset)
			s.failed = true
			return result, err
		}

		s.converted = s.converted[:0]
		if cap(s.converted) < out.Channels {
			s.converted = make([]float64, out.Channels)
		}
		n := s.channels.convertFrame(samples, s.converted)
		dst := make([]float64, n)
		copy(dst, s.converted[:n])

		outputFrames, resampleErr := s.resampler.push([][]float64{dst})
		encoded, encErr := s.encodeOutput(outputFrames)
		result = append(result, encoded...)
		if resampleErr != nil || encErr != nil {
			s.pending = append([]byte(nil), s.pending[offset+frameBytes:]...)
			s.inputOffset += int64(offset + frameBytes)
			s.failed = true
			if resampleErr != nil {
				return result, resampleErr
			}
			return result, encErr
		}
	}

	s.inputOffset += int64(consumable)
	s.pending = append([]byte(nil), s.pending[consumable:]...)
	return result, nil
}

// Close flushes filter delay. It reports ErrorIncompleteFrame when fewer than
// one full input frame is buffered. No valid input sample is dropped or
// duplicated by the resampling stage.
func (s *Stream) Close() ([]byte, error) {
	if s.closed {
		return nil, newStreamError(ErrorStreamClosed, s.inputOffset, -1, "Close called more than once")
	}
	s.closed = true
	if s.failed {
		return nil, newStreamError(ErrorStreamClosed, s.inputOffset, -1, "stream has already failed")
	}

	result := []byte{}
	if len(s.pending) > 0 {
		// The offset deliberately points at the start of the unusable tail so
		// callers can locate the exact boundary.
		completeFrames := s.inputOffset / int64(s.converter.in.frameBytes())
		err := newStreamError(ErrorIncompleteFrame, s.inputOffset, completeFrames,
			"stream ends with bytes that do not form a complete frame")
		s.failed = true
		return nil, err
	}

	tail, resampleErr := s.resampler.flush()
	result, err := s.encodeOutput(tail)
	if resampleErr != nil || err != nil {
		s.failed = true
		if resampleErr != nil {
			return result, resampleErr
		}
		return result, err
	}
	return result, nil
}

func (s *Stream) encodeOutput(frames [][]float64) ([]byte, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	flat := make([]float64, 0, len(frames)*s.converter.out.Channels)
	for _, frame := range frames {
		flat = append(flat, frame...)
	}
	return encodeSamples(flat, s.converter.out, s.converter.ditherEnabled(), s.dither)
}
