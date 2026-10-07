package pcm

// Config contains optional conversion semantics.
type Config struct {
	Dither   DitherConfig
	Resample ResampleConfig
}

// Option mutates a Config.
type Option func(*Config)

// Converter validates formats and creates independent streaming sessions.
type Converter struct {
	in  PCMFormat
	out PCMFormat
	cfg Config
}

// WithDither configures requantization behavior.
func WithDither(method DitherMethod, seed uint64) Option {
	return func(c *Config) {
		c.Dither = DitherConfig{Method: method, Seed: seed}
	}
}

// WithLinearResampling selects low-delay two-tap linear interpolation.
func WithLinearResampling() Option {
	return func(c *Config) {
		c.Resample = ResampleConfig{Method: ResampleLinear, HalfWidth: 0, Cutoff: 0}
	}
}

// WithSincResampling selects Hann-windowed sinc resampling. A halfWidth of zero
// selects the package default (32 input frames on each side); cutoff zero
// selects the safe Nyquist-derived cutoff automatically.
func WithSincResampling(halfWidth int, cutoff float64) Option {
	return func(c *Config) {
		c.Resample = ResampleConfig{
			Method:    ResampleSinc,
			HalfWidth: halfWidth,
			Cutoff:    cutoff,
		}
	}
}

// WithResampling installs a complete resampler configuration.
func WithResampling(cfg ResampleConfig) Option {
	return func(c *Config) { c.Resample = cfg }
}

// NewConverter validates a complete conversion description and rejects unknown,
// contradictory or unsupported combinations before stream data is accepted.
func NewConverter(in, out PCMFormat, opts ...Option) (*Converter, error) {
	cfg := Config{Resample: defaultResampleConfig()}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.Resample.Method == ResampleSinc && cfg.Resample.HalfWidth == 0 {
		cfg.Resample.HalfWidth = 32
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	if err := out.validate(); err != nil {
		return nil, err
	}
	if err := validateResample(cfg.Resample); err != nil {
		return nil, err
	}
	if err := validateDither(in, out, cfg.Dither); err != nil {
		return nil, err
	}
	return &Converter{in: in, out: out, cfg: cfg}, nil
}

func (c *Converter) ditherEnabled() bool {
	return shouldDither(c.in, c.out, c.cfg.Dither.Method)
}

// NewStream creates an independent streaming conversion session. Sessions do
// not share offsets, filters or dither state, so one stream's failure cannot
// corrupt another.
func (c *Converter) NewStream() *Stream {
	channels, _ := newChannelConverter(c.in, c.out)
	return &Stream{
		converter: c,
		channels:  channels,
		resampler: newMultiResampler(c.in.SampleRate, c.out.SampleRate,
			c.out.Channels, c.cfg.Resample),
		dither:    newDitherGenerator(c.cfg.Dither.Seed),
		converted: make([]float64, 0, c.out.Channels),
	}
}
