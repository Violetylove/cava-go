package audio

// Tests of the native protocol handshake against a mock PulseAudio
// server. The mock server is a real TCP listener speaking the wire
// protocol, so these tests run on any platform (no cgo, no daemon).

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// mockPAServer is a minimal fake PulseAudio server: it completes the
// three-step handshake and then streams a 440 Hz sine wave as float32le
// stereo over a record stream. When errCode is non-zero the stream
// creation is rejected with that error code.
type mockPAServer struct {
	t       *testing.T
	ln      net.Listener
	conn    net.Conn
	errCode uint32

	// Observed CREATE_RECORD_STREAM fields.
	gotSourceName string
	gotRate       uint32
	gotChannels   byte
	gotFormat     byte
}

func newMockPAServer(t *testing.T, errCode uint32) *mockPAServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock server listen: %v", err)
	}
	s := &mockPAServer{t: t, ln: ln, errCode: errCode}
	go s.serve()
	return s
}

func (s *mockPAServer) addr() string { return s.ln.Addr().String() }

func (s *mockPAServer) close() {
	s.ln.Close()
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *mockPAServer) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	s.conn = conn
	for i := 0; i < 3; i++ {
		f, err := readFrame(conn)
		if err != nil {
			s.t.Errorf("mock server read: %v", err)
			return
		}
		r := &tagReader{data: f.payload}
		cmd, err := r.u32()
		if err != nil {
			s.t.Errorf("mock server command: %v", err)
			return
		}
		tag, err := r.u32()
		if err != nil {
			s.t.Errorf("mock server tag: %v", err)
			return
		}
		switch cmd {
		case paCmdAuth:
			if err := s.reply(tag, func(w *tagWriter) { w.u32(35) }); err != nil {
				s.t.Errorf("mock server auth reply: %v", err)
				return
			}
		case paCmdSetClientName:
			if err := s.reply(tag, func(w *tagWriter) { w.u32(1) }); err != nil {
				s.t.Errorf("mock server name reply: %v", err)
				return
			}
		case paCmdCreateRecordStream:
			if s.errCode != 0 {
				if err := s.sendError(tag, s.errCode); err != nil {
					s.t.Errorf("mock server error reply: %v", err)
				}
				return
			}
			if err := s.parseRecordStream(r); err != nil {
				s.t.Errorf("mock server parse record stream: %v", err)
				return
			}
			if err := s.reply(tag, s.recordReply); err != nil {
				s.t.Errorf("mock server stream reply: %v", err)
				return
			}
			s.streamSine(conn)
			return
		default:
			s.t.Errorf("mock server unexpected command %d", cmd)
			return
		}
	}
}

func (s *mockPAServer) reply(tag uint32, build func(*tagWriter)) error {
	w := &tagWriter{}
	w.u32(paCmdReply)
	w.u32(tag)
	if build != nil {
		build(w)
	}
	return writeFrame(s.conn, channelControl, w.buf)
}

func (s *mockPAServer) sendError(tag, code uint32) error {
	w := &tagWriter{}
	w.u32(paCmdError)
	w.u32(tag)
	w.u32(code)
	return writeFrame(s.conn, channelControl, w.buf)
}

// parseRecordStream validates the client's CREATE_RECORD_STREAM payload
// against the full protocol >= 22 field order (see
// pulsecore/protocol-native.c command_create_record_stream): sample spec,
// channel map, source index, source name, maxlength, corked, fragsize,
// then the v12/v13/v14/v15/v22 flag groups.
func (s *mockPAServer) parseRecordStream(r *tagReader) error {
	t, err := r.tag()
	if err != nil {
		return err
	}
	if t != tagSampleSpec {
		return fmt.Errorf("expected sample spec, got tag %q", t)
	}
	if s.gotFormat, err = r.tag(); err != nil {
		return err
	}
	if s.gotChannels, err = r.tag(); err != nil {
		return err
	}
	if r.pos+4 > len(r.data) {
		return io.ErrUnexpectedEOF
	}
	s.gotRate = binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4

	if err := r.skip(); err != nil { // channel map
		return err
	}
	if v, err := r.u32(); err != nil {
		return err
	} else if v != paInvalidIndex { // source index
		return fmt.Errorf("source index = %d, want PA_INVALID_INDEX", v)
	}
	if s.gotSourceName, err = r.str(); err != nil {
		return err
	}
	if v, err := r.u32(); err != nil { // maxlength
		return err
	} else if v != paInvalidIndex {
		return fmt.Errorf("maxlength = %d, want PA_INVALID_INDEX", v)
	}
	if v, err := r.boolean(); err != nil { // corked
		return err
	} else if v {
		return fmt.Errorf("corked must be false")
	}
	if v, err := r.u32(); err != nil { // fragsize
		return err
	} else if v != paInvalidIndex {
		return fmt.Errorf("fragsize = %d, want PA_INVALID_INDEX", v)
	}

	// protocol v12: 7 booleans (no_remap .. variable_rate)
	for i := 0; i < 7; i++ {
		if v, err := r.boolean(); err != nil {
			return err
		} else if v {
			return fmt.Errorf("v12 flag %d must be false", i)
		}
	}
	// protocol v13: peak_detect, adjust_latency, proplist, direct_on_input
	if v, err := r.boolean(); err != nil {
		return err
	} else if v {
		return fmt.Errorf("peak_detect must be false")
	}
	if v, err := r.boolean(); err != nil {
		return err
	} else if !v {
		return fmt.Errorf("adjust_latency must be true")
	}
	if err := r.skip(); err != nil { // proplist
		return err
	}
	if v, err := r.u32(); err != nil { // direct_on_input
		return err
	} else if v != paInvalidIndex {
		return fmt.Errorf("direct_on_input = %d, want PA_INVALID_INDEX", v)
	}
	// protocol v14: early_requests
	if v, err := r.boolean(); err != nil {
		return err
	} else if v {
		return fmt.Errorf("early_requests must be false")
	}
	// protocol v15: dont_inhibit_auto_suspend, fail_on_suspend
	for i := 0; i < 2; i++ {
		if v, err := r.boolean(); err != nil {
			return err
		} else if v {
			return fmt.Errorf("v15 flag %d must be false", i)
		}
	}
	// protocol v22: n_formats, cvolume, muted, volume_set, muted_set,
	// relative_volume, passthrough
	if v, err := r.tag(); err != nil {
		return err
	} else if v != tagU8 {
		return fmt.Errorf("expected U8 n_formats, got tag %q", v)
	}
	if v, err := r.tag(); err != nil {
		return err
	} else if v != 0 {
		return fmt.Errorf("n_formats = %d, want 0", v)
	}
	if err := r.skip(); err != nil { // cvolume
		return err
	}
	for i := 0; i < 5; i++ {
		if v, err := r.boolean(); err != nil {
			return err
		} else if v {
			return fmt.Errorf("v22 flag %d must be false", i)
		}
	}
	if !r.eof() {
		return fmt.Errorf("trailing bytes in CREATE_RECORD_STREAM payload")
	}
	return nil
}

// recordReply mirrors the server's CREATE_RECORD_STREAM reply.
func (s *mockPAServer) recordReply(w *tagWriter) {
	w.u32(1)          // stream index
	w.u32(2)          // source output index
	w.u32(0xFFFFFFFF) // maxlength
	w.u32(0xFFFFFFFF) // fragsize
	w.sampleSpec(paSampleFloat32LE, 2, 48000)
	w.channelMap([]byte{paChPositionFrontLeft, paChPositionFrontRight})
	w.u32(3) // source index
	w.str("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor")
	w.boolean(false) // suspended
	// configured_source_latency (usec)
	w.buf = append(w.buf, tagUsec)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], 0)
	w.buf = append(w.buf, b[:]...)
	// negotiated format info
	w.buf = append(w.buf, tagFormatInfo, 0) // PA_ENCODING_INVALID
	w.proplist(nil)
}

// streamSine emits ~100 ms of 440 Hz sine as float32le stereo, then holds
// the connection open until the peer disconnects.
func (s *mockPAServer) streamSine(conn net.Conn) {
	const framesPerPacket = 480 // 10 ms at 48 kHz
	const packets = 10
	phase := 0.0
	for p := 0; p < packets; p++ {
		payload := make([]byte, framesPerPacket*2*4)
		for i := 0; i < framesPerPacket; i++ {
			v := float32(math.Sin(phase))
			phase += 2 * math.Pi * 440 / 48000
			binary.LittleEndian.PutUint32(payload[i*8:], math.Float32bits(v))
			binary.LittleEndian.PutUint32(payload[i*8+4:], math.Float32bits(v))
		}
		if err := writeFrame(conn, 1, payload); err != nil {
			return
		}
	}
	// Stay open so the client decides when to close (avoids a race on
	// which side's EOF is seen first).
	_, _ = readFrame(conn)
}

// sineFrame validates one received audio frame: 480 stereo frames whose
// mono-mixed samples follow a 440 Hz sine.
func checkSineFrame(t *testing.T, payload []byte) {
	t.Helper()
	const frames = 480
	if len(payload) != frames*2*4 {
		t.Fatalf("payload length = %d, want %d", len(payload), frames*2*4)
	}
	got := make([]float32, frames)
	convertFrame(got, &payload[0], 2, formatFloat32)
	for i, v := range got {
		want := float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
		if math.Abs(float64(v-want)) > 1e-4 {
			t.Fatalf("sample[%d] = %v, want %v", i, v, want)
		}
	}
}

func errOr(err error, msg string) error {
	if err != nil {
		return err
	}
	return &strErr{msg}
}

type strErr struct{ s string }

func (e *strErr) Error() string { return e.s }

var errUnexpectedEOF = &strErr{"unexpected EOF"}

func TestHandshakeAndDataAgainstMockServer(t *testing.T) {
	srv := newMockPAServer(t, 0)
	defer srv.close()

	conn, err := net.Dial("tcp", srv.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	r, err := handshake(conn, "cava-go", make([]byte, paNativeCookieLength), nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if srv.gotSourceName != "@DEFAULT_MONITOR@" {
		t.Errorf("source name = %q", srv.gotSourceName)
	}
	if srv.gotFormat != paSampleFloat32LE || srv.gotChannels != 2 || srv.gotRate != 48000 {
		t.Errorf("sample spec = fmt %d ch %d rate %d", srv.gotFormat, srv.gotChannels, srv.gotRate)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	f, err := readFrame(r)
	if err != nil {
		t.Fatalf("read audio frame: %v", err)
	}
	if f.channel == channelControl {
		t.Fatal("expected audio data message, got control packet")
	}
	checkSineFrame(t, f.payload)
}

func TestHandshakeRejectedByServer(t *testing.T) {
	srv := newMockPAServer(t, 5 /* PA_ERR_INVALID */)
	defer srv.close()

	conn, err := net.Dial("tcp", srv.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, err = handshake(conn, "cava-go", make([]byte, paNativeCookieLength), nil)
	if err == nil {
		t.Fatal("handshake succeeded, want server error")
	}
	if !strings.Contains(err.Error(), "server error 5") {
		t.Fatalf("error = %q, want it to mention server error 5", err.Error())
	}
}
