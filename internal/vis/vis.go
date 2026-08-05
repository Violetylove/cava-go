// Package vis defines visualization data contracts and drawable
// visualizations (spectrum, waveform, ...). See docs/DESIGN.md §5.5 and §6.1.
package vis

// Frame is the per-frame data consumed by visualizations.
type Frame struct {
	// Bars holds bar heights in [0, 1] for spectrum-based visuals.
	Bars []float32

	// Wave holds normalized time-domain samples for waveform visuals.
	Wave []float32
}
