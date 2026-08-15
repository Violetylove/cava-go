//go:build windows

package audio

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/go-ole/go-ole"
)

func TestParseFormatExtensibleFloat(t *testing.T) {
	// Build a C-layout WAVEFORMATEXTENSIBLE buffer (40 bytes).
	buf := make([]byte, 40)
	binary.LittleEndian.PutUint16(buf[offFormatTag:], waveFormatExtensibleTag)
	binary.LittleEndian.PutUint16(buf[offChannels:], 2)
	binary.LittleEndian.PutUint16(buf[offBitsPerSample:], 32)
	binary.LittleEndian.PutUint16(buf[offSize:], 22)
	binary.LittleEndian.PutUint16(buf[offExtSamples:], 32)
	binary.LittleEndian.PutUint32(buf[offExtChannelMask:], 0x3)
	copy(buf[offExtSubFormat:], guidBytes(ksDataFormatSubtypeIEEEFloat))

	ch, format, err := parseFormat(unsafe.Pointer(&buf[0]))
	if err != nil {
		t.Fatal(err)
	}
	if ch != 2 || format != formatFloat32 {
		t.Errorf("got ch=%d format=%d, want 2/%d", ch, format, formatFloat32)
	}
}

func TestParseFormatPCM16(t *testing.T) {
	buf := make([]byte, 18)
	binary.LittleEndian.PutUint16(buf[offFormatTag:], waveFormatPCM)
	binary.LittleEndian.PutUint16(buf[offChannels:], 1)
	binary.LittleEndian.PutUint16(buf[offBitsPerSample:], 16)
	ch, format, err := parseFormat(unsafe.Pointer(&buf[0]))
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 || format != formatInt16 {
		t.Errorf("got ch=%d format=%d, want 1/%d", ch, format, formatInt16)
	}
}

func TestParseFormatUnsupported(t *testing.T) {
	buf := make([]byte, 18)
	binary.LittleEndian.PutUint16(buf[offFormatTag:], 0xDEAD)
	binary.LittleEndian.PutUint16(buf[offChannels:], 2)
	binary.LittleEndian.PutUint16(buf[offBitsPerSample:], 8)
	if _, _, err := parseFormat(unsafe.Pointer(&buf[0])); err == nil {
		t.Error("expected error for unsupported format")
	}
}

// guidBytes returns the raw memory bytes of a GUID.
func guidBytes(g *ole.GUID) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(g)), 16)
}
