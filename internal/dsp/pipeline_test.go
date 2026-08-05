package dsp

import (
	"math"
	"testing"
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
