package audio

// Pure-Go PulseAudio native protocol client (protocol version >= 34,
// i.e. PulseAudio 14.0 and newer). The implementation is a minimal subset
// for capturing a record stream from the default output monitor:
//
//   frame:  20-byte big-endian descriptor (length, channel, offset_hi,
//           offset_lo, flags) followed by the payload
//   packet: descriptor with channel = 0xFFFFFFFF, payload is a tagstruct
//           (typed TLV) starting with [U32 command] [U32 tag]
//   audio:  descriptor with channel = stream index, payload is raw PCM
//
// Older protocol versions (PA <= 13) use a different command numbering and
// an untagged encoding; connecting to them fails with a clear error.
//
// References: PulseAudio 17.0 sources — pulsecore/pstream.c,
// pulsecore/tagstruct.c, pulsecore/native-common.h, pulse/context.c,
// pulse/stream.c, pulsecore/protocol-native.c.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Protocol constants.
const (
	// protocolVersion is the highest native protocol version this client
	// implements. Servers clamp it to their own version; 34 covers
	// PulseAudio 14.0 through current releases.
	protocolVersion = 34

	// paSampleFloat32LE is PA_SAMPLE_FLOAT32LE; the server resamples and
	// converts the monitor stream to our requested format.
	paSampleFloat32LE = 5

	paChPositionFrontLeft  = 1 // PA_CHANNEL_POSITION_FRONT_LEFT
	paChPositionFrontRight = 2 // PA_CHANNEL_POSITION_FRONT_RIGHT
	paVolumeNorm           = 0x10000

	paInvalidIndex = 0xFFFFFFFF // PA_INVALID_INDEX

	paNativeDefaultPort = 4713

	// paNativeCookieLength is PA_NATIVE_COOKIE_LENGTH.
	paNativeCookieLength = 256
)

// Command numbers (protocol >= 34; PA_COMMAND_* in native-common.h).
const (
	paCmdError              = 0
	paCmdReply              = 2
	paCmdCreateRecordStream = 5
	paCmdDeleteRecordStream = 6
	paCmdAuth               = 8
	paCmdSetClientName      = 9
)

// tagstruct type tags (PA_TAG_* in tagstruct.h).
const (
	tagString       = 't'
	tagStringNull   = 'N'
	tagU32          = 'L'
	tagU8           = 'B'
	tagU64          = 'R'
	tagSampleSpec   = 'a'
	tagArbitrary    = 'x'
	tagBooleanTrue  = '1'
	tagBooleanFalse = '0'
	tagUsec         = 'U'
	tagChannelMap   = 'm'
	tagCVolume      = 'v'
	tagProplist     = 'P'
	tagVolume       = 'V'
	tagFormatInfo   = 'f'
)

// pulseSampleRate is the sample rate requested from PulseAudio (it
// resamples the monitor stream to it).
const pulseSampleRate = 48000

// pstream descriptor layout: 5 big-endian uint32.
const (
	descriptorSize     = 20
	descriptorLength   = 0
	descriptorChannel  = 4
	descriptorOffsetHi = 8
	descriptorOffsetLo = 12
	descriptorFlags    = 16
	channelControl     = 0xFFFFFFFF // packets (commands/replies) use this channel
	maxFramePayload    = 1 << 20    // sanity cap for a single message
)

// tagWriter appends typed TLV fields to a command payload.
type tagWriter struct {
	buf []byte
}

func (w *tagWriter) u8(v byte) { w.buf = append(w.buf, tagU8, v) }
func (w *tagWriter) boolean(v bool) {
	if v {
		w.buf = append(w.buf, tagBooleanTrue)
	} else {
		w.buf = append(w.buf, tagBooleanFalse)
	}
}
func (w *tagWriter) u32(v uint32) {
	w.buf = append(w.buf, tagU32)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	w.buf = append(w.buf, b[:]...)
}
func (w *tagWriter) str(s string) {
	w.buf = append(w.buf, tagString)
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
}
func (w *tagWriter) arbitrary(p []byte) {
	w.buf = append(w.buf, tagArbitrary)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(len(p)))
	w.buf = append(w.buf, b[:]...)
	w.buf = append(w.buf, p...)
}
func (w *tagWriter) sampleSpec(format, channels byte, rate uint32) {
	w.buf = append(w.buf, tagSampleSpec, format, channels)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], rate)
	w.buf = append(w.buf, b[:]...)
}
func (w *tagWriter) channelMap(pos []byte) {
	w.buf = append(w.buf, tagChannelMap, byte(len(pos)))
	w.buf = append(w.buf, pos...)
}
func (w *tagWriter) cVolume(channels byte, values []uint32) {
	w.buf = append(w.buf, tagCVolume, channels)
	for _, v := range values {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		w.buf = append(w.buf, b[:]...)
	}
}
func (w *tagWriter) proplist(entries [][2]string) {
	w.buf = append(w.buf, tagProplist)
	for _, e := range entries {
		w.str(e[0])
		w.u32(uint32(len(e[1])))
		w.arbitrary([]byte(e[1]))
	}
	w.buf = append(w.buf, tagStringNull)
}

// tagReader decodes typed TLV fields from a received payload. Unknown
// fields are skipped via skip so replies with extra trailing data parse.
type tagReader struct {
	data []byte
	pos  int
}

func (r *tagReader) eof() bool { return r.pos >= len(r.data) }

func (r *tagReader) tag() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	t := r.data[r.pos]
	r.pos++
	return t, nil
}

func (r *tagReader) u32() (uint32, error) {
	if t, err := r.tag(); err != nil {
		return 0, err
	} else if t != tagU32 {
		return 0, fmt.Errorf("pulse: expected U32 field, got tag %q", t)
	}
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *tagReader) boolean() (bool, error) {
	t, err := r.tag()
	if err != nil {
		return false, err
	}
	switch t {
	case tagBooleanTrue:
		return true, nil
	case tagBooleanFalse:
		return false, nil
	default:
		return false, fmt.Errorf("pulse: expected boolean field, got tag %q", t)
	}
}

// skip advances past the current tagged field, whatever its type.
func (r *tagReader) skip() error {
	t, err := r.tag()
	if err != nil {
		return err
	}
	switch t {
	case tagU32, tagVolume:
		return r.advance(4)
	case tagU64, tagUsec:
		return r.advance(8)
	case tagU8:
		return r.advance(1)
	case tagString:
		return r.advanceString()
	case tagStringNull:
		return nil
	case tagArbitrary:
		if r.pos+4 > len(r.data) {
			return io.ErrUnexpectedEOF
		}
		l := int(binary.BigEndian.Uint32(r.data[r.pos:]))
		if err := r.advance(4); err != nil {
			return err
		}
		return r.advance(l)
	case tagSampleSpec:
		return r.advance(6) // format, channels, rate
	case tagChannelMap:
		if r.pos >= len(r.data) {
			return io.ErrUnexpectedEOF
		}
		n := int(r.data[r.pos])
		return r.advance(1 + n)
	case tagCVolume:
		if r.pos >= len(r.data) {
			return io.ErrUnexpectedEOF
		}
		n := int(r.data[r.pos])
		return r.advance(1 + 4*n)
	case tagProplist:
		for {
			t2, err := r.tag()
			if err != nil {
				return err
			}
			if t2 == tagStringNull {
				return nil
			}
			if t2 != tagString {
				return fmt.Errorf("pulse: malformed proplist (tag %q)", t2)
			}
			if err := r.advanceString(); err != nil {
				return err
			}
			// value length + arbitrary payload
			if _, err := r.u32(); err != nil {
				return err
			}
			if err := r.skip(); err != nil {
				return err
			}
		}
	case tagFormatInfo:
		// encoding u8 + proplist
		if err := r.advance(1); err != nil {
			return err
		}
		return r.skip()
	default:
		return fmt.Errorf("pulse: unknown tagstruct tag %q", t)
	}
}

// str reads a tagged string (tagString or tagStringNull).
func (r *tagReader) str() (string, error) {
	t, err := r.tag()
	if err != nil {
		return "", err
	}
	if t == tagStringNull {
		return "", nil
	}
	if t != tagString {
		return "", fmt.Errorf("pulse: expected string field, got tag %q", t)
	}
	i := r.pos
	for i < len(r.data) && r.data[i] != 0 {
		i++
	}
	if i >= len(r.data) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(r.data[r.pos:i])
	r.pos = i + 1
	return s, nil
}

func (r *tagReader) advance(n int) error {
	if r.pos+n > len(r.data) {
		return io.ErrUnexpectedEOF
	}
	r.pos += n
	return nil
}

func (r *tagReader) advanceString() error {
	i := r.pos
	for i < len(r.data) && r.data[i] != 0 {
		i++
	}
	if i >= len(r.data) {
		return io.ErrUnexpectedEOF
	}
	r.pos = i + 1
	return nil
}

// paFrame is one raw pstream message.
type paFrame struct {
	channel uint32
	payload []byte
}

// writeFrame writes a pstream frame (descriptor + payload).
func writeFrame(w io.Writer, channel uint32, payload []byte) error {
	var h [descriptorSize]byte
	binary.BigEndian.PutUint32(h[descriptorLength:], uint32(len(payload)))
	binary.BigEndian.PutUint32(h[descriptorChannel:], channel)
	// offset_hi, offset_lo and flags stay 0.
	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame reads one pstream frame from r.
func readFrame(r io.Reader) (paFrame, error) {
	var h [descriptorSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return paFrame{}, err
	}
	length := binary.BigEndian.Uint32(h[descriptorLength:])
	channel := binary.BigEndian.Uint32(h[descriptorChannel:])
	if length > maxFramePayload {
		return paFrame{}, fmt.Errorf("pulse: frame payload %d exceeds limit %d", length, maxFramePayload)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return paFrame{}, err
	}
	return paFrame{channel: channel, payload: payload}, nil
}

// pulseConn is a connected native-protocol session.
type pulseConn struct {
	conn net.Conn
	r    *bufio.Reader
	tag  uint32

	// firstSend, when set, replaces the write of the first command
	// packet. The platform layer uses it to attach SCM_CREDENTIALS to
	// the AUTH message on unix sockets (libpulse does the same; the
	// server then authenticates by uid instead of the cookie).
	firstSend func(payload []byte) error
}

// send writes a command packet, honoring firstSend for the first one.
func (c *pulseConn) send(cmd uint32, tag uint32, w *tagWriter) error {
	var buf bytes.Buffer
	if err := writeFrame(&buf, channelControl, w.buf); err != nil {
		return fmt.Errorf("pulse: encode command %d: %w", cmd, err)
	}
	frame := buf.Bytes()
	if c.firstSend != nil {
		fn := c.firstSend
		c.firstSend = nil
		if err := fn(frame); err != nil {
			return fmt.Errorf("pulse: send command %d: %w", cmd, err)
		}
		return nil
	}
	if _, err := c.conn.Write(frame); err != nil {
		return fmt.Errorf("pulse: send command %d: %w", cmd, err)
	}
	return nil
}

// roundTrip sends one command packet and blocks until the reply carrying
// the same tag arrives. onReply, when non-nil, is called with the reply
// payload (positioned after command+tag) before it is discarded.
func (c *pulseConn) roundTrip(cmd uint32, build func(*tagWriter), onReply func(*tagReader) error) error {
	c.tag++
	tag := c.tag
	w := &tagWriter{}
	w.u32(cmd)
	w.u32(tag)
	if build != nil {
		build(w)
	}
	if err := c.send(cmd, tag, w); err != nil {
		return err
	}
	for {
		f, err := readFrame(c.r)
		if err != nil {
			return fmt.Errorf("pulse: waiting reply for command %d: %w", cmd, err)
		}
		if f.channel != channelControl {
			return fmt.Errorf("pulse: audio data received before stream ready")
		}
		rr := &tagReader{data: f.payload}
		got, err := rr.u32()
		if err != nil {
			return fmt.Errorf("pulse: malformed reply header: %w", err)
		}
		gotTag, err := rr.u32()
		if err != nil {
			return fmt.Errorf("pulse: malformed reply tag: %w", err)
		}
		if gotTag != tag {
			continue // unrelated async message, ignore
		}
		switch got {
		case paCmdReply:
			if onReply != nil {
				if err := onReply(rr); err != nil {
					return err
				}
			}
			return nil
		case paCmdError:
			code, _ := rr.u32()
			return fmt.Errorf("pulse: server error %d for command %d", code, cmd)
		default:
			return fmt.Errorf("pulse: unexpected reply command %d", got)
		}
	}
}

// handshake authenticates and opens a record stream on the default output
// monitor. After it returns, audio data messages arrive on the connection.
// The returned reader MUST be used for all subsequent reads: it may have
// buffered bytes past the final reply.
// firstSend, when non-nil, is used to write the first (AUTH) command so
// the platform layer can attach SCM_CREDENTIALS on unix sockets.
func handshake(conn net.Conn, clientName string, cookie []byte, firstSend func(payload []byte) error) (*bufio.Reader, error) {
	r := bufio.NewReader(conn)
	c := &pulseConn{conn: conn, r: r, firstSend: firstSend}

	// AUTH: declare our protocol version (no SHM/MEMFD flags: the server
	// then ships audio data inline instead of via shared memory). The
	// reply carries the server's own protocol version; check it so an old
	// server fails here with a clear message instead of mid-stream.
	var serverVersion uint32
	if err := c.roundTrip(paCmdAuth, func(w *tagWriter) {
		w.u32(protocolVersion)
		w.arbitrary(cookie)
	}, func(r *tagReader) error {
		v, err := r.u32()
		if err != nil {
			return err
		}
		serverVersion = v & 0xFFFF // PA_PROTOCOL_VERSION_MASK
		return nil
	}); err != nil {
		return nil, fmt.Errorf("pulse: auth: %w", err)
	}
	if serverVersion < protocolVersion {
		return nil, fmt.Errorf("pulse: server protocol version %d < %d (PulseAudio 14.0+ required)", serverVersion, protocolVersion)
	}

	// SET_CLIENT_NAME: a proplist is expected since protocol v13.
	if err := c.roundTrip(paCmdSetClientName, func(w *tagWriter) {
		w.proplist([][2]string{{"application.name", clientName}})
	}, nil); err != nil {
		return nil, fmt.Errorf("pulse: set client name: %w", err)
	}

	// CREATE_RECORD_STREAM payload (protocol >= 22 layout, see
	// pulsecore/protocol-native.c command_create_record_stream).
	const channels = 2
	if err := c.roundTrip(paCmdCreateRecordStream, func(w *tagWriter) {
		w.sampleSpec(paSampleFloat32LE, channels, pulseSampleRate)
		w.channelMap([]byte{paChPositionFrontLeft, paChPositionFrontRight})
		w.u32(paInvalidIndex)      // source index: pick by name
		w.str("@DEFAULT_MONITOR@") // capture the default output monitor
		w.u32(paInvalidIndex)      // maxlength: server default
		w.boolean(false)           // corked
		w.u32(paInvalidIndex)      // fragsize: server default
		// protocol v12 flags (all off)
		for i := 0; i < 7; i++ {
			w.boolean(false)
		}
		// protocol v13
		w.boolean(false) // peak_detect
		w.boolean(true)  // adjust_latency
		w.proplist([][2]string{{"media.name", "cava-go capture"}})
		w.u32(paInvalidIndex) // direct_on_input
		// protocol v14
		w.boolean(false) // early_requests
		// protocol v15
		w.boolean(false) // dont_inhibit_auto_suspend
		w.boolean(false) // fail_on_suspend
		// protocol v22
		w.u8(0) // n_formats: plain sample spec above
		w.cVolume(channels, []uint32{paVolumeNorm, paVolumeNorm})
		w.boolean(false) // muted
		w.boolean(false) // volume_set
		w.boolean(false) // muted_set
		w.boolean(false) // relative_volume
		w.boolean(false) // passthrough
	}, nil); err != nil {
		return nil, fmt.Errorf("pulse: create record stream: %w", err)
	}
	return r, nil
}

// pulseServer lists the native protocol socket locations to try, in order
// (mirrors pa_context_connect's default server list).
func pulseServerCandidates() []string {
	var cands []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		cands = append(cands, filepath.Join(xdg, "pulse", "native"))
	}
	cands = append(cands,
		filepath.Join("/run/user", itoa(os.Getuid()), "pulse", "native"),
		filepath.Join(homeDir(), ".pulse", "native"),
		filepath.Join("/var", "run", "pulse", "native"),
	)
	return cands
}

// parsePulseServer resolves a PULSE_SERVER value into a network/address
// pair (pa_parse_address semantics: "unix:" prefix or a leading '/' mean a
// unix socket; anything else is TCP with PA_NATIVE_DEFAULT_PORT).
func parsePulseServer(s string) (network, address string) {
	switch {
	case strings.HasPrefix(s, "unix:"):
		return "unix", s[len("unix:"):]
	case strings.HasPrefix(s, "tcp:"), strings.HasPrefix(s, "tcp4:"):
		return "tcp", withDefaultPort(s[strings.Index(s, ":")+1:])
	case strings.HasPrefix(s, "tcp6:"):
		return "tcp", withDefaultPort(s[len("tcp6:"):])
	case strings.HasPrefix(s, "/"):
		return "unix", s
	default:
		return "tcp", withDefaultPort(s)
	}
}

// withDefaultPort appends PA_NATIVE_DEFAULT_PORT when addr has none.
func withDefaultPort(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if strings.Contains(addr, "]:") {
			return addr
		}
		return addr + ":" + itoa(paNativeDefaultPort)
	}
	if strings.Contains(addr, ":") {
		return addr
	}
	return addr + ":" + itoa(paNativeDefaultPort)
}

// pulseCookie loads the 256-byte auth cookie (all zero when absent).
func pulseCookie() []byte {
	cookie := make([]byte, paNativeCookieLength)
	var path string
	switch {
	case os.Getenv("PULSE_COOKIE") != "":
		path = os.Getenv("PULSE_COOKIE")
	case os.Getenv("XDG_CONFIG_HOME") != "":
		path = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "pulse", "cookie")
	default:
		path = filepath.Join(homeDir(), ".config", "pulse", "cookie")
	}
	if data, err := os.ReadFile(path); err == nil {
		copy(cookie, data)
	}
	return cookie
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/"
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
