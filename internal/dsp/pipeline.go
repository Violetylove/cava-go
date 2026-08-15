package dsp

import (
	"fmt"
	"math"
	"math/cmplx"
	"sync"
	"time"

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

	// AutoSens enables automatic gain that adapts to the incoming loudness
	// (peak-oriented: it drives the smoothed maximum bar toward TargetPeak).
	AutoSens bool
	// Sensitivity is a fixed multiplicative gain applied on top of (or
	// instead of, when AutoSens is off) the autosens gain (default 1).
	Sensitivity float64
	// TargetPeak is the smoothed peak bar level the autosens gain aims for,
	// in [0, 1] (default 0.8).
	TargetPeak float64

	// Falloff is the per-second peak decay rate in [0, ∞): each analysis
	// frame the previous bars drop by Falloff * (hop/sampleRate), so a
	// bar at 1.0 falls to 0 in ~1/Falloff seconds (default 2.0; 0 = off).
	Falloff float64
	// SmoothBars applies a [0.25 0.5 0.25] neighbor convolution to the
	// bars to reduce jaggedness (default true).
	SmoothBars bool
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
	// AutoSens / SmoothBars are explicit (default false in Go); the caller
	// opts in. Falloff 0 = off. Sensitivity 0 would multiply bars to zero,
	// so treat 0 as 1 (no fixed gain) unless autosens supplies gain.
	if c.Sensitivity == 0 {
		c.Sensitivity = 1
	}
	if c.TargetPeak == 0 {
		c.TargetPeak = 0.8
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
	instBars  []float32 // instantaneous (gained+smooth) bars
	latest    []float32 // displayed bars (with time-driven falloff)

	// falloff is driven by real time in Latest(), so bars keep falling
	// even when the audio stream stalls (no new Process calls).
	lastData time.Time  // last Process call
	lastTick time.Time  // last Latest call
	now      func() time.Time // clock (injectable for tests)

	// autosens state
	smoothPeak float64
	gain       float64
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
		instBars:   make([]float32, cfg.Bars),
		latest:     make([]float32, cfg.Bars),
		windowMean: windowMean(win),
		gain:       cfg.Sensitivity,
		now:        time.Now,
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

	p.lastData = p.now()
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

// instFloor drops instantaneous bar values below this threshold to zero.
// Near-silence residual amplified by the autosens gain lands in this band;
// without the floor, the peak-hold logic in Latest() would pin a falling
// bar top at a dim 1px sliver (visible as "residue cells" while falling).
const instFloor = 0.03

// Latest returns a copy of the most recent bar frame (0..1 per bar) with
// time-driven falloff applied: each call decays the displayed bars by
// Falloff * elapsed time, so they fall back even when the audio stream has
// stopped feeding data (which would otherwise freeze the bars).
func (p *Pipeline) Latest() []float32 {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if p.lastTick.IsZero() {
		p.lastTick = now
	}
	dt := now.Sub(p.lastTick).Seconds()
	p.lastTick = now

	// Data is stale when no PCM frame arrived for two analysis periods;
	// treat the input as silence so bars decay to zero.
	stale := !p.lastData.IsZero() &&
		now.Sub(p.lastData).Seconds() > 2*float64(p.cfg.Hop)/p.cfg.SampleRate

	decay := p.cfg.Falloff * dt
	out := make([]float32, len(p.latest))
	for i := range p.latest {
		inst := p.instBars[i]
		if stale || inst < instFloor {
			inst = 0
		}
		v := p.latest[i] - float32(decay)
		if inst > v {
			v = inst // classic peak-hold: rise instantly, decay slowly
		}
		if v < 0 {
			v = 0
		}
		p.latest[i] = v
		out[i] = v
	}
	return out
}

// analyze performs one FFT on the most recent FFTSize samples and updates
// instBars through the chain: raw -> gain (autosens) -> smooth. Falloff is
// applied by Latest on the displayed bars. Caller must hold p.mu.
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

	// 1) raw bars
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
		p.instBars[b] = v
	}

	// 2) gain (peak-oriented autosens)
	g := p.updateGain()
	for b := range p.instBars {
		v := p.instBars[b] * float32(g)
		if v > 1 {
			v = 1
		}
		p.instBars[b] = v
	}

	// 3) neighbor smoothing on the instantaneous bars
	if p.cfg.SmoothBars {
		copy(p.instBars, applySmooth(p.instBars))
	}
}

// applySmooth convolves bars with a [0.25 0.5 0.25] kernel; out-of-range
// neighbors are treated as the edge bar itself (edges are not pulled down).
func applySmooth(bars []float32) []float32 {
	out := make([]float32, len(bars))
	last := len(bars) - 1
	for b := range bars {
		prev := bars[max(b-1, 0)]
		cur := bars[b]
		next := bars[min(b+1, last)]
		out[b] = 0.25*prev + 0.5*cur + 0.25*next
	}
	return out
}

// updateGain returns the gain applied to the current frame. With autosens
// enabled it drives the smoothed peak bar toward TargetPeak: because the
// peak bar itself is multiplied by the gain, this keeps the tallest bar
// near the target height regardless of input loudness (within clamps).
//
// Responsiveness: the smoothed peak follows quickly (α=0.2) and the gain
// uses asymmetric attack/release — it rises fast (α=0.3, ~3 analyses ≈ 64ms)
// so bars track transients immediately, but falls slowly (α=0.05) to avoid
// pumping on momentary loud peaks. This keeps perceived latency well under
// ~150ms (a previous symmetric α=0.05 caused ~0.6s lag, reported as ~1s).
func (p *Pipeline) updateGain() float64 {
	if !p.cfg.AutoSens {
		return p.cfg.Sensitivity
	}
	peak := float64(0)
	for _, v := range p.instBars {
		if float64(v) > peak {
			peak = float64(v)
		}
	}
	if p.smoothPeak == 0 {
		p.smoothPeak = peak
	} else {
		p.smoothPeak += 0.2 * (peak - p.smoothPeak)
	}
	g := p.cfg.TargetPeak / math.Max(p.smoothPeak, 1e-9)
	g = math.Max(0.2, math.Min(15, g))
	if g > p.gain {
		p.gain += 0.3 * (g - p.gain) // attack: track rising loudness fast
	} else {
		p.gain += 0.05 * (g - p.gain) // release: ease down slowly
	}
	if p.gain < 0.2 {
		p.gain = 0.2
	}
	return p.gain * p.cfg.Sensitivity
}
