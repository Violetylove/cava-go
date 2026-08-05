package audio

import "math"

// RMS returns the root-mean-square energy of a PCM frame, in [0, 1]
// for normalized samples. It is used for silence detection and gain.
func RMS(samples []float32) float32 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v) * float64(v)
	}
	return float32(math.Sqrt(sum / float64(len(samples))))
}
