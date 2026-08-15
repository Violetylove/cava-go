//go:build windows

package audio

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"
)

const (
	// waitTimeout is the Win32 WAIT_TIMEOUT return of WaitForSingleObject.
	waitTimeout = 0x102
	// eventPollInterval bounds how long the capture loop may block on the
	// audio event before it re-checks the stop signal (ms).
	eventPollInterval = 500

	// audclntSBufferEmpty is the AUDCLNT_S_BUFFER_EMPTY success code returned
	// by IAudioCaptureClient::GetBuffer when no data is currently available.
	// go-wca reports it as an error (it treats any nonzero HRESULT as failure),
	// so we detect it via ole.OleError.Code and treat it as an empty packet.
	audclntSBufferEmpty = 0x08890001
)

// WasapiSource is an AudioSource that captures system output audio via
// WASAPI loopback on the default render endpoint (shared, event-driven).
// See docs/DESIGN.md §4.
type WasapiSource struct {
	mu      sync.Mutex
	started bool

	stop   chan struct{}
	done   chan struct{}
	frames chan []float32

	sampleRate int
	closeErr   error
}

// NewWasapiSource returns a new WASAPI loopback source.
func NewWasapiSource() *WasapiSource {
	return &WasapiSource{}
}

// Start begins loopback capture and returns a channel of mono float32
// frames in [-1, 1]. The channel is closed when capture stops or fails.
func (s *WasapiSource) Start() (<-chan []float32, error) {
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

// SampleRate returns the capture sample rate in Hz (from the device mix
// format). Valid after Start has returned successfully.
func (s *WasapiSource) SampleRate() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sampleRate
}

// Close stops capture and releases all resources. It is safe to call
// multiple times; the returned error reflects the capture loop's final
// state (nil on clean stop).
func (s *WasapiSource) Close() error {
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

// isBufferEmpty reports whether err is the AUDCLNT_S_BUFFER_EMPTY success
// code, which go-wca surfaces as an error.
func isBufferEmpty(err error) bool {
	var oe *ole.OleError
	if errors.As(err, &oe) {
		return oe.Code() == audclntSBufferEmpty
	}
	return false
}

// run drives the whole capture lifecycle on one goroutine so COM
// initialization, use and uninitialization all happen on the same thread.
func (s *WasapiSource) run(ready chan<- error) {
	err := s.captureLoop(ready)
	s.mu.Lock()
	s.closeErr = err
	s.started = false
	s.mu.Unlock()
	close(s.frames)
	close(s.done)
}

func (s *WasapiSource) captureLoop(ready chan<- error) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		ready <- fmt.Errorf("CoInitializeEx: %w", err)
		return nil
	}
	defer ole.CoUninitialize()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		ready <- fmt.Errorf("create MMDeviceEnumerator: %w", err)
		return nil
	}
	defer enumerator.Release()

	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &device); err != nil {
		ready <- fmt.Errorf("get default render endpoint: %w", err)
		return nil
	}
	defer device.Release()

	var audioClient *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &audioClient); err != nil {
		ready <- fmt.Errorf("activate IAudioClient: %w", err)
		return nil
	}
	defer audioClient.Release()

	var mixFormat *wca.WAVEFORMATEX
	if err := audioClient.GetMixFormat(&mixFormat); err != nil {
		ready <- fmt.Errorf("get mix format: %w", err)
		return nil
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(mixFormat)))

	channels, format, err := parseFormat(unsafe.Pointer(mixFormat))
	if err != nil {
		ready <- err
		return nil
	}
	s.sampleRate = int(mixFormat.NSamplesPerSec)

	flags := uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK | wca.AUDCLNT_STREAMFLAGS_EVENTCALLBACK)
	if err := audioClient.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, 0, 0, mixFormat, nil); err != nil {
		ready <- fmt.Errorf("initialize audio client: %w", err)
		return nil
	}

	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		ready <- fmt.Errorf("create event: %w", err)
		return nil
	}
	defer windows.CloseHandle(event)

	if err := audioClient.SetEventHandle(uintptr(event)); err != nil {
		ready <- fmt.Errorf("set event handle: %w", err)
		return nil
	}

	var captureClient *wca.IAudioCaptureClient
	if err := audioClient.GetService(wca.IID_IAudioCaptureClient, &captureClient); err != nil {
		ready <- fmt.Errorf("get IAudioCaptureClient: %w", err)
		return nil
	}
	defer captureClient.Release()

	if err := audioClient.Start(); err != nil {
		ready <- fmt.Errorf("start audio client: %w", err)
		return nil
	}
	defer audioClient.Stop()

	ready <- nil

	// Event-driven pump: wait for the audio engine, then drain all
	// available packets. Frames are dropped (not blocked) when the
	// consumer is slow, favoring real-time freshness over completeness.
	for {
		select {
		case <-s.stop:
			return nil
		default:
		}

		if w := wca.WaitForSingleObject(uintptr(event), eventPollInterval); w == waitTimeout {
			continue
		}
		for {
			var data *byte
			var frames uint32
			var flags uint32
			if err := captureClient.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
				if isBufferEmpty(err) {
					// No data available yet; wait for the next event.
					break
				}
				return fmt.Errorf("get buffer: %w", err)
			}
			if frames == 0 {
				break
			}
			frame := make([]float32, int(frames))
			if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 {
				convertFrame(frame, data, channels, format)
			}
			if err := captureClient.ReleaseBuffer(frames); err != nil {
				return fmt.Errorf("release buffer: %w", err)
			}
			select {
			case s.frames <- frame:
			default: // drop frame, keep the pipeline real-time
			}
		}
	}
}
