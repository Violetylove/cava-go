//go:build windows

package audio

import (
	"fmt"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// WAVE_FORMAT_* tags (mmreg.h).
const (
	waveFormatPCM           = 0x0001
	waveFormatIEEEFloat     = 0x0003
	waveFormatExtensibleTag = 0xFFFE
)

// Offsets into the C WAVEFORMATEX / WAVEFORMATEXTENSIBLE layouts.
//
// We read the mix format via fixed byte offsets instead of struct mapping:
// Go rounds struct sizes up to the alignment (WAVEFORMATEX is 18 bytes in C
// but 20 bytes as a Go struct), which would shift every following field.
const (
	offFormatTag     = 0  // WORD  wFormatTag
	offChannels      = 2  // WORD  nChannels
	offSamplesPerSec = 4  // DWORD nSamplesPerSec
	offBlockAlign    = 12 // WORD  nBlockAlign
	offBitsPerSample = 14 // WORD  wBitsPerSample
	offSize          = 16 // WORD  cbSize
	// WAVEFORMATEXTENSIBLE tail (only valid when cbSize >= 22):
	offExtSamples     = 18 // WORD  wValidBitsPerSample
	offExtChannelMask = 20 // DWORD dwChannelMask
	offExtSubFormat   = 24 // GUID  SubFormat (16 bytes)
)

var (
	ksDataFormatSubtypePCM       = ole.NewGUID("{00000001-0000-0010-8000-00AA00389B71}")
	ksDataFormatSubtypeIEEEFloat = ole.NewGUID("{00000003-0000-0010-8000-00AA00389B71}")
)

// parseFormat inspects the mix format (a C WAVEFORMATEX/WAVEFORMATEXTENSIBLE
// buffer) and returns the channel count and sample format of the stream.
func parseFormat(p unsafe.Pointer) (channels int, format pcmFormat, err error) {
	channels = int(*(*uint16)(unsafe.Add(p, offChannels)))
	if channels <= 0 {
		return 0, formatUnknown, fmt.Errorf("invalid channel count %d", channels)
	}
	tag := *(*uint16)(unsafe.Add(p, offFormatTag))
	bits := *(*uint16)(unsafe.Add(p, offBitsPerSample))
	cb := *(*uint16)(unsafe.Add(p, offSize))

	switch {
	case tag == waveFormatExtensibleTag && cb >= 22:
		sub := *(*ole.GUID)(unsafe.Add(p, offExtSubFormat))
		switch sub {
		case *ksDataFormatSubtypeIEEEFloat:
			format = formatFloat32
		case *ksDataFormatSubtypePCM:
			format = pcmFormatFromBits(bits)
		default:
			return 0, formatUnknown, fmt.Errorf("unsupported extensible subformat %v (cb=%d)", sub, cb)
		}
	case tag == waveFormatIEEEFloat:
		format = formatFloat32
	case tag == waveFormatPCM:
		format = pcmFormatFromBits(bits)
	}
	if format == formatUnknown {
		return 0, formatUnknown, fmt.Errorf("unsupported mix format: tag=0x%X bits=%d cb=%d", tag, bits, cb)
	}
	return channels, format, nil
}
