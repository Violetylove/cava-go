package audio

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestTagWriterEncoding(t *testing.T) {
	var w tagWriter
	w.u32(34)
	w.boolean(true)
	w.boolean(false)
	w.u8(0)
	w.str("@DEFAULT_MONITOR@")
	w.arbitrary([]byte{1, 2, 3})
	w.sampleSpec(paSampleFloat32LE, 2, 48000)
	w.channelMap([]byte{paChPositionFrontLeft, paChPositionFrontRight})
	w.cVolume(2, []uint32{paVolumeNorm, paVolumeNorm})
	w.proplist([][2]string{{"media.name", "cava-go"}})

	want := []byte{
		'L', 0, 0, 0, 34,
		'1', '0', 'B', 0,
		't', '@', 'D', 'E', 'F', 'A', 'U', 'L', 'T', '_', 'M', 'O', 'N', 'I', 'T', 'O', 'R', '@', 0,
		'x', 0, 0, 0, 3, 1, 2, 3,
		'a', 5, 2, 0, 0, 0xBB, 0x80,
		'm', 2, 1, 2,
		'v', 2, 0, 1, 0, 0, 0, 1, 0, 0,
		'P', 't', 'm', 'e', 'd', 'i', 'a', '.', 'n', 'a', 'm', 'e', 0, 'L', 0, 0, 0, 7, 'x', 0, 0, 0, 7, 'c', 'a', 'v', 'a', '-', 'g', 'o', 'N',
	}
	if !bytes.Equal(w.buf, want) {
		t.Fatalf("encoding mismatch:\n got %v\nwant %v", w.buf, want)
	}
}

func TestTagReaderRoundTrip(t *testing.T) {
	var w tagWriter
	w.u32(0xDEADBEEF)
	w.boolean(true)
	w.boolean(false)
	w.str("hello")
	w.u8(7)
	w.arbitrary([]byte{9, 8, 7})

	r := &tagReader{data: w.buf}
	if v, err := r.u32(); err != nil || v != 0xDEADBEEF {
		t.Fatalf("u32 = %#x, %v", v, err)
	}
	if v, err := r.boolean(); err != nil || !v {
		t.Fatalf("boolean = %v, %v", v, err)
	}
	if v, err := r.boolean(); err != nil || v {
		t.Fatalf("boolean = %v, %v", v, err)
	}
	if !r.eof() {
		// Skip the string, u8 and arbitrary; then we must be at EOF.
		if err := r.skip(); err != nil {
			t.Fatalf("skip string: %v", err)
		}
		if err := r.skip(); err != nil {
			t.Fatalf("skip u8: %v", err)
		}
		if err := r.skip(); err != nil {
			t.Fatalf("skip arbitrary: %v", err)
		}
		if !r.eof() {
			t.Fatalf("expected EOF after skipping all fields, pos=%d len=%d", r.pos, len(r.data))
		}
	}
}

func TestTagReaderSkipProplist(t *testing.T) {
	var w tagWriter
	w.proplist([][2]string{{"a", "1"}, {"b", "22"}})
	w.u32(99)

	r := &tagReader{data: w.buf}
	if err := r.skip(); err != nil {
		t.Fatalf("skip proplist: %v", err)
	}
	if v, err := r.u32(); err != nil || v != 99 {
		t.Fatalf("u32 after proplist = %d, %v", v, err)
	}
}

func TestTagReaderSkipU32(t *testing.T) {
	// skip() must advance exactly past a U32 (4 bytes) so following
	// fields stay aligned.
	var w tagWriter
	w.u32(0x11223344)
	w.u32(0xAABBCCDD)
	w.str("after")

	r := &tagReader{data: w.buf}
	if err := r.skip(); err != nil {
		t.Fatalf("skip u32: %v", err)
	}
	if v, err := r.u32(); err != nil || v != 0xAABBCCDD {
		t.Fatalf("u32 after skip = %#x, %v", v, err)
	}
	if err := r.skip(); err != nil { // the string
		t.Fatalf("skip string: %v", err)
	}
	if !r.eof() {
		t.Fatalf("expected EOF, pos=%d len=%d", r.pos, len(r.data))
	}
}

func TestTagReaderTruncated(t *testing.T) {
	var w tagWriter
	w.u32(1)
	w.str("payload")
	// cut=0 is the intact payload and must parse fine; any truncation of
	// the trailing string must fail.
	for _, cut := range []int{0, 1, 4} {
		r := &tagReader{data: w.buf[:len(w.buf)-cut]}
		if _, err := r.u32(); err != nil {
			t.Fatalf("cut=%d: unexpected u32 error %v", cut, err)
		}
		err := r.skip()
		if cut == 0 && err != nil {
			t.Fatalf("cut=0: skip failed: %v", err)
		}
		if cut > 0 && err == nil {
			t.Fatalf("cut=%d: expected error, got nil", cut)
		}
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{1, 2, 3, 4, 5}
	if err := writeFrame(&buf, channelControl, payload); err != nil {
		t.Fatal(err)
	}
	// Control channel packet: 20-byte descriptor then payload.
	if buf.Len() != descriptorSize+len(payload) {
		t.Fatalf("frame size = %d, want %d", buf.Len(), descriptorSize+len(payload))
	}
	f, err := readFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.channel != channelControl || !bytes.Equal(f.payload, payload) {
		t.Fatalf("frame = %+v", f)
	}
	// Audio data frame (channel = stream index).
	buf.Reset()
	if err := writeFrame(&buf, 7, payload); err != nil {
		t.Fatal(err)
	}
	f, err = readFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.channel != 7 {
		t.Fatalf("channel = %d, want 7", f.channel)
	}
	// Truncated frame must error.
	if _, err := readFrame(&buf); err == nil {
		t.Fatal("expected error on truncated frame")
	}
}

func TestParsePulseServer(t *testing.T) {
	cases := []struct {
		in, network, address string
	}{
		{"unix:/tmp/pulse/native", "unix", "/tmp/pulse/native"},
		{"/run/user/1000/pulse/native", "unix", "/run/user/1000/pulse/native"},
		{"tcp:127.0.0.1:4713", "tcp", "127.0.0.1:4713"},
		{"tcp4:127.0.0.1", "tcp", "127.0.0.1:4713"},
		{"tcp6:[::1]", "tcp", "[::1]:4713"},
		{"127.0.0.1:1234", "tcp", "127.0.0.1:1234"},
		{"localhost", "tcp", "localhost:4713"},
	}
	for _, c := range cases {
		n, a := parsePulseServer(c.in)
		if n != c.network || a != c.address {
			t.Errorf("parsePulseServer(%q) = (%q, %q), want (%q, %q)", c.in, n, a, c.network, c.address)
		}
	}
}

func TestPulseServerCandidates(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	cands := pulseServerCandidates()
	if len(cands) < 2 {
		t.Fatalf("expected >=2 candidates, got %v", cands)
	}
	if got := filepath.Clean(cands[0]); got != filepath.Clean("/run/user/1000/pulse/native") {
		t.Fatalf("first candidate = %q", cands[0])
	}
}

func TestPulseCookieMissing(t *testing.T) {
	t.Setenv("PULSE_COOKIE", "/nonexistent/cookie")
	c := pulseCookie()
	if len(c) != paNativeCookieLength {
		t.Fatalf("cookie length = %d", len(c))
	}
	for i, b := range c {
		if b != 0 {
			t.Fatalf("cookie[%d] = %d, want 0", i, b)
		}
	}
}

func TestCommandPayloadLayout(t *testing.T) {
	// The command header must be [U32 command][U32 tag] so the server's
	// pdispatch can read it (pulsecore/pdispatch.c).
	var w tagWriter
	w.u32(paCmdAuth)
	w.u32(42)
	w.u32(protocolVersion)
	w.arbitrary(make([]byte, paNativeCookieLength))

	if len(w.buf) != 1+4+1+4+1+4+1+4+paNativeCookieLength {
		t.Fatalf("unexpected command length %d", len(w.buf))
	}
	r := &tagReader{data: w.buf}
	cmd, err := r.u32()
	if err != nil || cmd != paCmdAuth {
		t.Fatalf("command = %d, %v", cmd, err)
	}
	tag, err := r.u32()
	if err != nil || tag != 42 {
		t.Fatalf("tag = %d, %v", tag, err)
	}
}
