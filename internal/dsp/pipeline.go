package dsp

import (
	"fmt"
	"math"
	"math/cmplx"
	"sync"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Config configures a Pipeline.
type Config struct {
	FFTSize    int     // FFT window size in samples (default 2048)
	SampleRate float64 // capture sample rate in Hz (required)
	Bars       int     // number of spectrum bars (default 64)
	MinFreq    float64 // lowest analyzed frequency (default 20)
	MaxFreq    float64 // highest analyzed frequency (default 20000)
	Hop        int     // samples between FFT analyses (default FFTSize/2)
}

func (c *Config) defaults() {
	if c.FFTSize <= 0 {
		c.FFTSize = 2048
	}
	if c.Bars <= 0 {
		c.Bars = 64
	}
	if c.MinFreq <= 0 {
		c.MinFreq = 20
	}
	if c.MaxFreq <= c.MinFreq {
		c.MaxFreq = 20000
	}
	if c.Hop <= 0 {
		c.Hop = c.FFTSize / 2
	}
}

// Pipeline consumes mono PCM frames and produces a stream of spectrum
// bar frames. It is safe for concurrent use: Process is called by the
// capture goroutine, Latest by the render goroutine.
type Pipeline struct {
	mu sync.Mutex

	cfg    Config
	window []float64
	fft    *fourier.FFT

	// windowMean is the Hann window's coherent gain; magnitudes are
	// divided by it so a full-scale sine reaches ~1.0 after windowing.
	windowMean float64

	ring    []float32 // ring buffer of recent samples
	ringPos int
	hopAcc  int // new samples accumulated since the last analysis

	binRanges [][2]int // [start, end) FFT bins per bar
	mags      []float64
	latest    []float32
}

// New builds a Pipeline with the given config.
func New(cfg Config) (*Pipeline, error) {
	cfg.defaults()
	if cfg.SampleRate <= 0 {
		return nil, fmt.Errorf("dsp: sample rate must be positive")
	}
	if cfg.Hop > cfg.FFTSize {
		return nil, fmt.Errorf("dsp: hop (%d) must not exceed fft size (%d)", cfg.Hop, cfg.FFTSize)
	}

	win := HannWindow(cfg.FFTSize)
	p := &Pipeline{
		cfg:        cfg,
		window:     win,
		fft:        fourier.NewFFT(cfg.FFTSize),
		ring:       make([]float32, cfg.FFTSize),
		binRanges:  make([][2]int, cfg.Bars),
		mags:       make([]float64, cfg.FFTSize/2+1),
		latest:     make([]float32, cfg.Bars),
		windowMean: windowMean(win),
	}
	p.buildBinRanges()
	return p, nil
}

// buildBinRanges maps each bar to a range of FFT bins on a logarithmic
// frequency axis between MinFreq and MaxFreq (cava-style).
func (p *Pipeline) buildBinRanges() {
	cfg := p.cfg
	ratio := cfg.MaxFreq / cfg.MinFreq
	maxBin := cfg.FFTSize / 2
	for b := 0; b < cfg.Bars; b++ {
		fLo := cfg.MinFreq * math.Pow(ratio, float64(b)/float64(cfg.Bars))
		fHi := cfg.MinFreq * math.Pow(ratio, float64(b+1)/float64(cfg.Bars))
		lo := int(math.Floor(fLo / cfg.SampleRate * float64(cfg.FFTSize)))
		hi := int(math.Ceil(fHi / cfg.SampleRate * float64(cfg.FFTSize)))
		if lo < 1 { // skip DC bin
			lo = 1
		}
		if hi > maxBin {
			hi = maxBin
		}
		if hi <= lo {
			hi = lo + 1
		}
		p.binRanges[b] = [2]int{lo, hi}
	}
}

// Process appends a PCM frame to the ring buffer and runs an FFT analysis
// whenever Hop new samples have accumulated. The latest bar frame is
// updated; slower consumers simply observe the newest values.
func (p *Pipeline) Process(frame []float32) {
	if len(frame) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, v := range frame {
		p.ring[p.ringPos] = v
		p.ringPos = (p.ringPos + 1) % len(p.ring)
	}
	p.hopAcc += len(frame)
	for p.hopAcc >= p.cfg.Hop {
		p.analyze()
		p.hopAcc -= p.cfg.Hop
	}
}

// Latest returns a copy of the most recent bar frame (0..1 per bar).
func (p *Pipeline) Latest() []float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]float32, len(p.latest))
	copy(out, p.latest)
	return out
}

// analyze performs one FFT on the most recent FFTSize samples and updates
// latest. Caller must hold p.mu.
func (p *Pipeline) analyze() {
	// Reconstruct the most recent FFTSize samples in chronological order
	// from the ring buffer, apply the window.
	real := make([]float64, p.cfg.FFTSize)
	n := len(p.ring)
	for i := 0; i < n; i++ {
		idx := (p.ringPos - n + i + 2*n) % n
		real[i] = float64(p.ring[idx]) * p.window[i]
	}

	spec := p.fft.Coefficients(nil, real)
	for k := 0; k <= p.cfg.FFTSize/2; k++ {
		m := cmplx.Abs(spec[k])
		if k > 0 {
			m *= 2 // account for the mirrored negative frequencies
		}
		p.mags[k] = m / float64(p.cfg.FFTSize) / p.windowMean
	}

	for b := range p.binRanges {
		lo, hi := p.binRanges[b][0], p.binRanges[b][1]
		var sum float64
		for k := lo; k < hi; k++ {
			sum += p.mags[k]
		}
		v := float32(sum / float64(hi-lo))
		if v > 1 {
			v = 1
		}
		p.latest[b] = v
	}
}
