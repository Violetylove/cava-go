package vis

import "testing"

// BenchmarkRenderSpectrum measures one full grid render at 120x30.
func BenchmarkRenderSpectrum(b *testing.B) {
	bars := make([]float32, 51)
	for i := range bars {
		bars[i] = float32(i%10) / 10
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RenderSpectrum(bars, 120, 30)
	}
}
