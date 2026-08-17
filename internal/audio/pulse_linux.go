//go:build linux

package audio

// PulseSource is an AudioSource that captures the system audio output on
// Linux via PulseAudio: it records from the default output monitor
// ("@DEFAULT_MONITOR@", i.e. whatever the default sink is playing).
//
// The native protocol client in pulse_proto.go is a pure-Go implementation
// (no cgo), so the Linux build cross-compiles with CGO_ENABLED=0.
//
// The sample format is requested as float32le / 48 kHz / 2 channels;
// PulseAudio performs the resampling and conversion, so the stream is
// handled by the shared convertFrame mono-mix.

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const pulseChannels = 2

// framesPerPacket is the size of each frame delivered on the frames
// channel (~10 ms of audio at 48 kHz). The server sends record data in
// large bursts, so incoming blocks are sliced into these packets: this
// keeps Process() calls frequent enough for the DSP pipeline's
// staleness detection (staleDataTimeout) and matches the old pa_simple
// loop.
const framesPerPacket = 480

// NewPulseSource returns a new PulseAudio monitor source.
func NewPulseSource() *PulseSource {
	return &PulseSource{}
}

// PulseSource implements AudioSource on Linux.
type PulseSource struct {
	mu      sync.Mutex
	started bool

	conn     net.Conn
	frames   chan []float32
	done     chan struct{}
	stopping atomic.Bool

	closeErr error
}

// Start begins capturing the default output monitor.
func (s *PulseSource) Start() (<-chan []float32, error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil, fmt.Errorf("capture already started")
	}
	s.frames = make(chan []float32, 4)
	s.done = make(chan struct{})
	s.started = true
	s.stopping.Store(false)
	s.mu.Unlock()

	ready := make(chan error, 1)
	go s.run(ready)
	if err := <-ready; err != nil {
		<-s.done
		return nil, err
	}
	return s.frames, nil
}

// Close stops capture and closes the connection. Safe to call multiple
// times.
func (s *PulseSource) Close() error {
	s.mu.Lock()
	started := s.started
	if started {
		s.stopping.Store(true)
		if s.conn != nil {
			s.conn.Close()
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

// run drives the capture lifecycle on one goroutine.
func (s *PulseSource) run(ready chan<- error) {
	err := s.captureLoop(ready)
	if err != nil && s.stopping.Load() {
		err = nil // closed by us: not an error
	}
	s.mu.Lock()
	s.closeErr = err
	s.started = false
	s.mu.Unlock()
	close(s.frames)
	close(s.done)
}

// captureLoop dials, handshakes and pumps audio frames until the
// connection fails or Close is called.
func (s *PulseSource) captureLoop(ready chan<- error) error {
	conn, err := dialPulseServer()
	if err != nil {
		ready <- err
		return nil
	}
	s.conn = conn
	defer conn.Close()

	r, err := handshake(conn, "cava-go", pulseCookie(), unixCredsSender(conn))
	if err != nil {
		ready <- err
		return nil
	}
	ready <- nil

	var pending []float32 // leftover mono samples across data blocks
	for {
		f, err := readFrame(r)
		if err != nil {
			return fmt.Errorf("pulse: read: %w", err)
		}
		if f.channel == channelControl {
			if err := handleControl(f); err != nil {
				return err
			}
			continue
		}
		// Audio data: interleaved float32le stereo, one frame per sample.
		n := len(f.payload) / (pulseChannels * 4)
		if n == 0 {
			continue
		}
		mono := make([]float32, n)
		convertFrame(mono, &f.payload[0], pulseChannels, formatFloat32)
		pending = append(pending, mono...)
		for len(pending) >= framesPerPacket {
			frame := make([]float32, framesPerPacket)
			copy(frame, pending[:framesPerPacket])
			pending = pending[framesPerPacket:]
			select {
			case s.frames <- frame:
			default: // drop frame, keep the pipeline real-time
			}
		}
	}
}

// handleControl processes one control packet received after the stream is
// up. Replies and asynchronous server events are ignored; only an explicit
// error terminates the capture.
func handleControl(f paFrame) error {
	rr := &tagReader{data: f.payload}
	cmd, err := rr.u32()
	if err != nil {
		return fmt.Errorf("pulse: malformed control message: %w", err)
	}
	switch cmd {
	case paCmdReply:
		return nil
	case paCmdError:
		if _, err := rr.u32(); err != nil { // tag
			return fmt.Errorf("pulse: malformed error packet: %w", err)
		}
		code, _ := rr.u32()
		return fmt.Errorf("pulse: server error %d", code)
	default:
		// Asynchronous server event (started/suspended/moved/...); the
		// stream keeps running, so just ignore it.
		return nil
	}
}

// dialPulseServer connects to PULSE_SERVER (a list is tried in order) or
// the default socket list.
func dialPulseServer() (net.Conn, error) {
	if env := os.Getenv("PULSE_SERVER"); env != "" {
		addrs := strings.FieldsFunc(env, func(r rune) bool {
			return r == ' ' || r == ';' || r == ','
		})
		if len(addrs) > 0 {
			var errs []string
			for _, a := range addrs {
				conn, err := dial(a)
				if err == nil {
					return conn, nil
				}
				errs = append(errs, fmt.Sprintf("%s: %v", a, err))
			}
			return nil, fmt.Errorf("pulse: no usable server from PULSE_SERVER (%s)", strings.Join(errs, "; "))
		}
	}
	var errs []string
	for _, cand := range pulseServerCandidates() {
		conn, err := dial(cand)
		if err == nil {
			return conn, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", cand, err))
	}
	return nil, fmt.Errorf("pulse: no usable server socket (%s)", strings.Join(errs, "; "))
}

// dial opens one connection to a server address.
func dial(server string) (net.Conn, error) {
	network, addr := parsePulseServer(server)
	return net.DialTimeout(network, addr, 5*time.Second)
}

// unixCredsSender returns a writer for the first command packet that
// attaches SCM_CREDENTIALS on unix sockets, mirroring libpulse: the
// server authenticates the peer by uid instead of the auth cookie. It
// returns nil for non-unix connections (TCP relies on auth-anonymous or
// the cookie).
func unixCredsSender(conn net.Conn) func(payload []byte) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil
	}
	return func(payload []byte) error {
		raw, err := uc.SyscallConn()
		if err != nil {
			return err
		}
		var oob []byte
		var ctlErr error
		if err := raw.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
				ctlErr = err
				return
			}
			oob = unix.UnixCredentials(&unix.Ucred{
				Pid: int32(unix.Getpid()),
				Uid: uint32(os.Getuid()),
				Gid: uint32(os.Getgid()),
			})
		}); err != nil {
			return err
		}
		if ctlErr != nil {
			return ctlErr
		}
		_, _, err = uc.WriteMsgUnix(payload, oob, nil)
		return err
	}
}
