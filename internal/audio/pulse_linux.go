//go:build linux

package audio

/*
#cgo LDFLAGS: -lpulse-simple -lpulse
#include <stdlib.h>
#include <pulse/simple.h>
#include <pulse/error.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

// PulseSource is an AudioSource that captures the system audio output on
// Linux via PulseAudio: it records from the default output monitor
// ("@DEFAULT_MONITOR@", i.e. whatever the default sink is playing).
//
// The sample format is requested as float32le / 48 kHz / 2 channels;
// PulseAudio performs the resampling and conversion, so the stream is
// handled by the shared convertFrame mono-mix.
type PulseSource struct {
	mu      sync.Mutex
	started bool

	stop   chan struct{}
	done   chan struct{}
	frames chan []float32

	simple     *C.pa_simple
	sampleRate int
	closeErr   error
}

const (
	pulseSampleRate = 48000
	pulseChannels   = 2
	// framesPerPacket is read per loop iteration (~10 ms of audio), so the
	// loop can poll the stop channel instead of blocking forever.
	framesPerPacket = 480
)

// NewPulseSource returns a new PulseAudio monitor source.
func NewPulseSource() *PulseSource {
	return &PulseSource{}
}

// Start begins capturing the default output monitor.
func (s *PulseSource) Start() (<-chan []float32, error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil, fmt.Errorf("capture already started")
	}
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.frames = make(chan []float32, 4)
	s.started = true
	s.mu.Unlock()

	ready := make(chan error, 1)
	go s.run(ready)
	if err := <-ready; err != nil {
		<-s.done
		return nil, err
	}
	return s.frames, nil
}

// Close stops capture and releases the PulseAudio connection. Safe to call
// multiple times.
func (s *PulseSource) Close() error {
	s.mu.Lock()
	started := s.started
	if started {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	s.mu.Unlock()
	if !started {
		return s.closeErr
	}
	<-s.done
	s.mu.Lock()
	err := s.closeErr
	s.mu.Unlock()
	return err
}

// SampleRate returns the capture sample rate (requested 48 kHz; PulseAudio
// resamples the monitor stream to it).
func (s *PulseSource) SampleRate() int {
	return pulseSampleRate
}

// run opens the PulseAudio record stream and pumps PCM packets. All
// pa_simple calls happen on this goroutine (pa_simple is not safe for
// concurrent use).
func (s *PulseSource) run(ready chan<- error) {
	err := s.captureLoop(ready)
	s.mu.Lock()
	s.closeErr = err
	s.started = false
	s.mu.Unlock()
	close(s.frames)
	close(s.done)
}

func (s *PulseSource) captureLoop(ready chan<- error) error {
	defer func() {
		if s.simple != nil {
			C.pa_simple_free(s.simple)
			s.simple = nil
		}
	}()

	var ss C.pa_sample_spec
	ss.format = C.PA_SAMPLE_FLOAT32LE
	ss.rate = C.uint32_t(pulseSampleRate)
	ss.channels = C.uint8_t(pulseChannels)

	dev := C.CString("@DEFAULT_MONITOR@")
	name := C.CString("cava-go")
	stream := C.CString("cava-go capture")
	defer C.free(unsafe.Pointer(dev))
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(stream))

	var cerr C.int
	s.simple = C.pa_simple_new(
		nil, name, C.PA_STREAM_RECORD, dev, stream,
		&ss, nil, nil, &cerr)
	if s.simple == nil {
		ready <- fmt.Errorf("pulseaudio: %s", C.GoString(C.pa_strerror(cerr)))
		return nil
	}

	s.sampleRate = pulseSampleRate
	ready <- nil

	// Read ~10 ms packets; the read blocks until full, so stop is polled
	// between packets.
	bytesPerPacket := framesPerPacket * pulseChannels * 4 // float32
	buf := make([]byte, bytesPerPacket)
	mono := make([]float32, framesPerPacket)
	for {
		select {
		case <-s.stop:
			return nil
		default:
		}
		n := C.pa_simple_read(s.simple, unsafe.Pointer(&buf[0]), C.size_t(len(buf)), &cerr)
		if n < 0 {
			return fmt.Errorf("pulseaudio read: %s", C.GoString(C.pa_strerror(cerr)))
		}
		convertFrame(mono, &buf[0], pulseChannels, formatFloat32)
		select {
		case s.frames <- mono:
		default: // drop frame, keep the pipeline real-time
		}
	}
}
