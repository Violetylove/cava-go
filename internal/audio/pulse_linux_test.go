//go:build linux

package audio

// End-to-end tests of PulseSource against the mock server. These exercise
// the full lifecycle (dial, handshake, frame pump, Close) and require
// linux because PulseSource is only built there.

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestPulseSourceAgainstMockServer(t *testing.T) {
	srv := newMockPAServer(t, 0)
	defer srv.close()
	t.Setenv("PULSE_SERVER", "tcp:"+srv.addr())

	src := NewPulseSource()
	frames, err := src.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer src.Close()

	if src.SampleRate() != 48000 {
		t.Fatalf("SampleRate = %d", src.SampleRate())
	}

	select {
	case f := <-frames:
		if len(f) != 480 {
			t.Fatalf("frame length = %d, want 480", len(f))
		}
		for i, v := range f {
			want := float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
			if math.Abs(float64(v-want)) > 1e-4 {
				t.Fatalf("sample[%d] = %v, want %v", i, v, want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for audio frame")
	}

	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestPulseSourceRejectedByServer(t *testing.T) {
	srv := newMockPAServer(t, 5 /* PA_ERR_INVALID */)
	defer srv.close()
	t.Setenv("PULSE_SERVER", "tcp:"+srv.addr())

	src := NewPulseSource()
	if _, err := src.Start(); err == nil {
		t.Fatal("Start succeeded, want server error")
	}
}

func TestPulseSourceUnreachableServer(t *testing.T) {
	// Nothing listens on port 1; Start must fail cleanly.
	t.Setenv("PULSE_SERVER", "tcp:127.0.0.1:1")
	src := NewPulseSource()
	if _, err := src.Start(); err == nil {
		t.Fatal("Start succeeded, want connection error")
	}
}

func TestHandleControlIgnoresAsyncEvents(t *testing.T) {
	// Server events (started/suspended/moved/...) arriving on the control
	// channel must not terminate the capture; only an explicit error does.
	var w tagWriter
	w.u32(90) // arbitrary async command number (not REPLY/ERROR)
	w.u32(0xFFFFFFFF)
	w.u32(7) // stream index
	if err := handleControl(paFrame{channel: channelControl, payload: w.buf}); err != nil {
		t.Fatalf("async event must be ignored, got %v", err)
	}

	// ERROR still terminates.
	w = tagWriter{}
	w.u32(paCmdError)
	w.u32(0)
	w.u32(5)
	err := handleControl(paFrame{channel: channelControl, payload: w.buf})
	if err == nil || !strings.Contains(err.Error(), "server error 5") {
		t.Fatalf("error packet must fail, got %v", err)
	}
}
