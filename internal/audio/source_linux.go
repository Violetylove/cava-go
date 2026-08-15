//go:build linux

package audio

// NewSource returns the platform's system-audio capture source.
func NewSource() AudioSource {
	return NewPulseSource()
}
