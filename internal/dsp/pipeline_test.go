package dsp

import (
	"math"
	"testing"
	"time"
)

func TestHannWindow(t *testing.T) {
	w := HannWindow(9) // odd length: center sample i=4 is the exact peak
	if w[0] != 0 || w[8] != 0 {
		t.Errorf("window endpoints should be 0, got %v %v", w[0], w[8])
	}
	if math.Abs(w[4]-1) > 1e-9 {
		t.Errorf("window center should be 1, got %v", w[4])
	}
	for _, v := range w {
		if v < 0 || v > 1 {
			t.Errorf("window out of [0,1]: %v", v)
		}
	}
}

func TestPipelineSilence(t *testing.T) {
	p, err := New(Config{SampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		p.Process(make([]float32, 480))
	}
	bars := p.Latest()
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("silence should produce zero bars, bar %d = %v", i, v)
		}
	}
}

func TestPipelineSine(t *testing.T) {
	const (
		sr      = 48000.0
		freq    = 1000.0
		fftSize = 2048
		numBars = 64
	)
	p, err := New(Config{FFTSize: fftSize, SampleRate: sr, Bars: numBars})
	if err != nil {
		t.Fatal(err)
	}
	// Feed a 1 kHz sine for ~1 second.
	frame := make([]float32, 480)
	for i := 0; i < 100; i++ {
		for j := range frame {
			tm := float64(i*len(frame)+j) / sr
			frame[j] = float32(0.5 * math.Sin(2*math.Pi*freq*tm))
		}
		p.Process(frame)
	}
	bars := p.Latest()
	// Find the bar with maximum energy; it must sit at the log-scale
	// position of 1 kHz (≈ bar 36 of 64 on 20Hz..20kHz) and be dominant.
	maxIdx, maxVal := 0, bars[0]
	for i, v := range bars {
		if v > maxVal {
			maxIdx, maxVal = i, v
		}
	}
	if maxVal < 0.1 {
		t.Fatalf("expected a strong peak for 1kHz sine, got max=%v", maxVal)
	}
	if maxIdx < 28 || maxIdx > 44 {
		t.Errorf("1kHz peak expected around bar 36, got index %d", maxIdx)
	}
	for i, v := range bars {
		if i != maxIdx && v > maxVal*0.5 {
			t.Errorf("bar %d = %v is too close to the peak %v", i, v, maxVal)
		}
	}
}

func TestPipelineBinRangesMonotonic(t *testing.T) {
	p, err := New(Config{FFTSize: 2048, SampleRate: 48000, Bars: 10})
	if err != nil {
		t.Fatal(err)
	}
	prevLo, prevHi := 0, 0
	for b, r := range p.binRanges {
		if r[0] < prevLo || r[1] < prevHi {
			t.Fatalf("bin range %d (%v) not monotonic", b, r)
		}
		if r[1] <= r[0] {
			t.Fatalf("bin range %d (%v) empty", b, r)
		}
		prevLo, prevHi = r[0], r[1]
	}
}

// feedSine pushes len*frames of a sine at the given amplitude.
func feedSine(p *Pipeline, amp float64, frames int) {
	const sr = 48000.0
	const freq = 1000.0
	frame := make([]float32, 480)
	for i := 0; i < frames; i++ {
		for j := range frame {
			tm := float64(i*len(frame)+j) / sr
			frame[j] = float32(amp * math.Sin(2*math.Pi*freq*tm))
		}
		p.Process(frame)
	}
}

func maxBar(bars []float32) float32 {
	m := float32(0)
	for _, v := range bars {
		if v > m {
			m = v
		}
	}
	return m
}

func TestAutoSensBoost(t *testing.T) {
	// A quiet signal (amp 0.1) must be boosted so the peak bar is clearly
	// visible (raw peak would be ~0.02).
	p, err := New(Config{SampleRate: 48000, AutoSens: true, SmoothBars: false})
	if err != nil {
		t.Fatal(err)
	}
	feedSine(p, 0.1, 150)
	if got := maxBar(p.Latest()); got < 0.15 {
		t.Errorf("autosens should boost quiet signal, max bar = %v", got)
	}
}

func TestAutoSensTargetsPeak(t *testing.T) {
	// A loud signal (amp 0.9) must be attenuated so the peak bar lands
	// near the target (0.8) instead of slamming into 1.0.
	p, err := New(Config{SampleRate: 48000, AutoSens: true, SmoothBars: false})
	if err != nil {
		t.Fatal(err)
	}
	feedSine(p, 0.9, 150)
	if got := maxBar(p.Latest()); got < 0.4 || got > 0.95 {
		t.Errorf("autosens should keep peak near target, got %v", got)
	}
}

func TestAutoSensAttack(t *testing.T) {
	// From silence, a burst must raise bars within a few analyses
	// (attack α=0.3 ≈ 64ms; the old symmetric α=0.05 caused ~0.6s lag).
	p, err := New(Config{SampleRate: 48000, AutoSens: true, SmoothBars: false})
	if err != nil {
		t.Fatal(err)
	}
	silence := make([]float32, 1024)
	for i := 0; i < 40; i++ {
		p.Process(silence) // settle at silence
	}
	feedSine(p, 0.3, 7) // ~3 analyses of a burst
	if got := maxBar(p.Latest()); got < 0.4 {
		t.Errorf("bars must rise fast after a burst, got %v", got)
	}
}

func TestFalloffDecay(t *testing.T) {
	// Bars must decay by Falloff * elapsed time per Latest() call even
	// without new audio data (time-driven, not analysis-driven).
	p, err := New(Config{SampleRate: 48000, AutoSens: false, SmoothBars: false, Falloff: 2.0})
	if err != nil {
		t.Fatal(err)
	}
	fake := time.Now()
	p.now = func() time.Time { return fake }
	feedSine(p, 1.0, 60)
	p.Latest() // initialize lastTick
	peak0 := maxBar(p.Latest())
	if peak0 < 0.1 {
		t.Fatalf("expected measurable peak, got %v", peak0)
	}

	prev := peak0
	for i := 0; i < 5; i++ {
		fake = fake.Add(50 * time.Millisecond)
		cur := maxBar(p.Latest())
		diff := float64(prev - cur)
		if cur > prev+1e-6 {
			t.Fatalf("bars must not rise during silence: %v -> %v", prev, cur)
		}
		// 2.0/s * 0.05s = 0.1 per step (skip the frame that hits zero).
		if cur > 0.001 && (diff < 0.08 || diff > 0.12) {
			t.Fatalf("decay per 50ms = %v, want ~0.1 (prev=%v cur=%v)", diff, prev, cur)
		}
		prev = cur
	}
}

func TestFalloffTimeDriven(t *testing.T) {
	// When the audio stream stops feeding data entirely, bars must still
	// fall back to zero (previously the analysis-driven falloff froze).
	p, err := New(Config{SampleRate: 48000, AutoSens: false, SmoothBars: false, Falloff: 3.0})
	if err != nil {
		t.Fatal(err)
	}
	fake := time.Now()
	p.now = func() time.Time { return fake }
	feedSine(p, 0.9, 60)
	p.Latest()
	peak0 := maxBar(p.Latest())
	if peak0 < 0.1 {
		t.Fatalf("expected a peak, got %v", peak0)
	}
	// Stop feeding data; advance one second in small steps.
	for i := 0; i < 20; i++ {
		fake = fake.Add(50 * time.Millisecond)
		p.Latest()
	}
	if got := maxBar(p.Latest()); got > 0.01 {
		t.Errorf("bars must fall back to zero after audio stops, got %v", got)
	}
}

func TestApplySmooth(t *testing.T) {
	got := applySmooth([]float32{0, 1, 0, 0})
	want := []float32{0.25, 0.5, 0.25, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("smooth[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// Edge bars are not pulled down by out-of-range neighbors.
	got = applySmooth([]float32{1, 0})
	if got[0] != 0.75 { // 0.25*1 + 0.5*1 + 0.25*0
		t.Errorf("edge bar = %v, want 0.75", got[0])
	}
}
