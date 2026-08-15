package dsp

import "testing"

// BenchmarkPipelineProcess measures one Process call (480-sample PCM frame
// at 48 kHz, i.e. a 10 ms audio packet).
func BenchmarkPipelineProcess(b *testing.B) {
	p, err := New(Config{SampleRate: 48000, AutoSens: true, SmoothBars: true})
	if err != nil {
		b.Fatal(err)
	}
	frame := make([]float32, 480)
	for i := range frame {
		frame[i] = 0.1
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Process(frame)
	}
}
