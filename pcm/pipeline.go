package pcm

import "io"

// Options configures a Pipeline.
type Options struct {
	Quality    Quality    // resampling quality/latency trade-off
	Dither     DitherMode // dither applied only when reducing bit depth
	DitherSeed uint64     // seed for reproducible dither
}

// Pipeline converts a stream of PCM bytes from In to Out format.
//
// Usage: feed arbitrarily sized chunks with Write, drain converted bytes
// with Read, and call Close at end of input to flush the resampler tail.
// The produced sample stream depends only on the input bytes, never on
// how they were chunked. A Pipeline is not safe for concurrent use, and
// an error on one Pipeline never affects any other.
type Pipeline struct {
	In  Format
	Out Format

	mix  *mixer
	rs   *resampler // nil when sample rates are equal
	dith *ditherer  // nil when no dither may be applied

	pending []byte // undecoded tail of a partial input frame
	pendOff int64  // absolute input offset of pending[0]
	outBuf  []byte // converted output ready for Read
	closed  bool
}

// NewPipeline validates the formats and builds a conversion pipeline.
// Unsupported combinations are rejected with an *Error of kind
// ErrKindUnsupported instead of silently producing wrong samples.
func NewPipeline(in, out Format, opts Options) (*Pipeline, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	if opts.Dither != DitherNone && opts.Dither != DitherTPDF {
		return nil, &Error{Kind: ErrKindUnsupported, Offset: -1,
			Msg: "unknown dither mode"}
	}
	p := &Pipeline{In: in, Out: out, mix: newMixer(in.Channels, out.Channels)}
	if in.SampleRate != out.SampleRate {
		p.rs = newResampler(in.SampleRate, out.SampleRate, out.Channels, opts.Quality)
	}
	// Dither is only meaningful when reducing precision: integer output
	// whose bit depth is below the source precision. Upsampling or
	// same-depth conversion never gets extra noise.
	reducing := out.SampleFormat == SampleFormatInt &&
		(in.SampleFormat == SampleFormatFloat || in.BitDepth > out.BitDepth)
	if reducing && opts.Dither != DitherNone {
		p.dith = newDitherer(opts.Dither, opts.DitherSeed)
	}
	return p, nil
}

// Write consumes a chunk of input bytes. On success it returns len(buf);
// a trailing partial frame is buffered internally and must not be resent.
//
// On a decode failure the returned *Error carries the absolute input
// offset of the offending sample, and the return value is the number of
// bytes of buf that were consumed (whole frames before the failing one).
// The failing frame and everything after it is dropped, so the caller may
// skip or repair the bad bytes and continue writing — the pipeline stays
// usable and other pipelines are never affected.
func (p *Pipeline) Write(buf []byte) (int, error) {
	if p.closed {
		return 0, &Error{Kind: ErrKindUnsupported, Offset: -1, Msg: "write after close"}
	}
	fs := p.In.FrameSize()
	combined := append(p.pending, buf...)
	base := p.pendOff
	nframes := len(combined) / fs

	inF := make([]float64, 0, nframes*p.In.Channels)
	for i := 0; i < nframes; i++ {
		off := base + int64(i*fs)
		frame := combined[i*fs : (i+1)*fs]
		for ch := 0; ch < p.In.Channels; ch++ {
			v, err := decodeSample(p.In, frame[ch*p.In.BytesPerSample():])
			if err != nil {
				err.(*Error).Offset = off + int64(ch*p.In.BytesPerSample())
				// drop the failing frame and everything after it
				p.pending = p.pending[:0]
				p.pendOff = off
				consumed := i*fs - (len(combined) - len(buf))
				if consumed < 0 {
					consumed = 0
				}
				return consumed, err
			}
			inF = append(inF, v)
		}
	}
	// all complete frames decoded; keep the partial tail for next time
	p.pending = append(p.pending[:0], combined[nframes*fs:]...)
	p.pendOff = base + int64(nframes*fs)
	consumed := len(buf)

	// channel mix
	mixed := inF
	if p.In.Channels != p.Out.Channels {
		mixed = make([]float64, 0, nframes*p.Out.Channels)
		tmp := make([]float64, p.Out.Channels)
		for i := 0; i < nframes; i++ {
			p.mix.apply(inF[i*p.In.Channels:(i+1)*p.In.Channels], tmp)
			mixed = append(mixed, tmp...)
		}
	}

	// resample (or pass through when rates match)
	var outF []float64
	if p.rs != nil {
		outF = p.rs.process(mixed, nil, false)
	} else {
		outF = mixed
	}
	p.encode(outF)
	return consumed, nil
}

// encode appends converted output bytes for the given interleaved frames.
func (p *Pipeline) encode(frames []float64) {
	bps := p.Out.BytesPerSample()
	var tmp [8]byte
	for _, v := range frames {
		// encodeSample never fails: values are clamped to range
		_ = encodeSample(p.Out, v, p.dith, tmp[:bps])
		p.outBuf = append(p.outBuf, tmp[:bps]...)
	}
}

// Read drains converted output bytes, returning the number of bytes read.
// It returns (0, nil) when no output is available yet, and io.EOF only
// after Close has been called and all remaining output has been drained.
func (p *Pipeline) Read(buf []byte) (int, error) {
	if len(p.outBuf) == 0 {
		if p.closed {
			return 0, io.EOF
		}
		return 0, nil
	}
	n := copy(buf, p.outBuf)
	p.outBuf = p.outBuf[n:]
	return n, nil
}

// Close signals end of input; the resampler tail is flushed and
// subsequent Read calls will drain the remaining output then return
// io.EOF. If the input ends mid-frame, Close returns an *Error of kind
// ErrKindIncompleteFrame pointing at the dangling bytes and does NOT
// close, so the caller may still supply the missing bytes.
func (p *Pipeline) Close() error {
	if p.closed {
		return nil
	}
	if len(p.pending) > 0 {
		return &Error{Kind: ErrKindIncompleteFrame, Offset: p.pendOff,
			Msg: "input stream ends with a partial frame"}
	}
	if p.rs != nil {
		p.encode(p.rs.process(nil, nil, true))
	}
	p.closed = true
	return nil
}
