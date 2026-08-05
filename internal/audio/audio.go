// Package audio provides audio capture sources. The concrete WASAPI
// loopback implementation lands in M1; DSP and render layers must only
// depend on the AudioSource interface (see docs/DESIGN.md §3.3).
package audio

// AudioSource captures system audio output and delivers normalized mono
// PCM frames as float32 samples in [-1, 1].
type AudioSource interface {
	// Start begins capture and returns a channel of PCM frames.
	// The channel is closed when capture stops or fails.
	Start() (<-chan []float32, error)

	// SampleRate returns the capture sample rate in Hz. Valid after Start.
	SampleRate() int

	// Close stops capture and releases resources.
	Close() error
}
