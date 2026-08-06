// Command cava-go is a Windows console audio visualizer (cava clone).
//
// M2 milestone build: capture system audio (WASAPI loopback), transform it
// via FFT into spectrum bars (internal/dsp) and render them to the terminal
// with block glyphs (internal/render). Press q / Esc / Ctrl-C to quit.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"cava-go/internal/audio"
	"cava-go/internal/dsp"
	"cava-go/internal/render"
)

func main() {
	duration := flag.Duration("duration", 0, "auto-exit after duration (0 = run until quit)")
	flag.Parse()

	src := audio.NewWasapiSource()
	frames, err := src.Start()
	if err != nil {
		log.Fatal("capture failed:", err)
	}
	defer src.Close()

	renderer, err := render.New(render.Config{FPS: 30})
	if err != nil {
		log.Fatal("terminal init failed:", err)
	}
	defer renderer.Fini()

	pipe, err := dsp.New(dsp.Config{
		FFTSize:     2048,
		SampleRate:  float64(src.SampleRate()),
		Bars:        51, // user preference: 20% fewer than 64 for a cleaner look
		MinFreq:     20,
		MaxFreq:     20000,
		AutoSens:    true,
		Sensitivity: 1.0,
		TargetPeak:  0.8,
		Falloff:     3.0,
		SmoothBars:  true,
	})
	if err != nil {
		log.Fatal("dsp init failed:", err)
	}

	// capture goroutine: audio frames -> DSP pipeline.
	go func() {
		for frame := range frames {
			pipe.Process(frame)
		}
	}()

	// stop channel: Ctrl-C / duration timer / renderer quit key all close it.
	stop := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		select {
		case <-sig:
		case <-timerOrNever(*duration):
		}
		close(stop)
	}()

	renderer.Run(pipe.Latest, stop)
}

// timerOrNever returns a channel that fires after d, or never when d <= 0.
func timerOrNever(d time.Duration) <-chan time.Time {
	if d <= 0 {
		return make(chan time.Time)
	}
	return time.After(d)
}
