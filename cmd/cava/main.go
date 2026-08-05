// Command cava-go is a Windows console audio visualizer (cava clone).
//
// M1 milestone build: capture system audio via WASAPI loopback and print
// per-interval RMS energy to verify the audio chain. The full pipeline
// (DSP + rendering) replaces this in M2.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"cava-go/internal/audio"
)

func main() {
	duration := flag.Duration("duration", 10*time.Second, "how long to capture")
	flag.Parse()

	src := audio.NewWasapiSource()
	frames, err := src.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "capture failed:", err)
		os.Exit(1)
	}
	defer src.Close()

	report := time.NewTicker(200 * time.Millisecond)
	defer report.Stop()
	timer := time.NewTimer(*duration)
	defer timer.Stop()

	var sum, peak float32
	var n int
	reportLine := func() {
		if n == 0 {
			fmt.Println("no frames")
			return
		}
		fmt.Printf("frames=%-6d avgRMS=%.4f peakRMS=%.4f\n", n, sum/float32(n), peak)
		sum, peak, n = 0, 0, 0
	}

	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				fmt.Println("capture ended")
				reportLine()
				if err := src.Close(); err != nil {
					fmt.Fprintln(os.Stderr, "capture error:", err)
				}
				return
			}
			rms := audio.RMS(frame)
			sum += rms
			if rms > peak {
				peak = rms
			}
			n++
		case <-report.C:
			reportLine()
		case <-timer.C:
			reportLine()
			fmt.Println("done")
			if err := src.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "capture error:", err)
			}
			return
		}
	}
}
