package dsp

import "math"

// HannWindow returns a Hann window of length n, scaled to [0, 1].
// It tapers to zero at both ends to reduce spectral leakage.
func HannWindow(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return w
}

// windowMean returns the arithmetic mean of the window (its coherent gain).
func windowMean(w []float64) float64 {
	var sum float64
	for _, v := range w {
		sum += v
	}
	return sum / float64(len(w))
}
