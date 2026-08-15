package audio

import "unsafe"

// pcmFormat identifies the sample format of a captured stream.
type pcmFormat int

const (
	formatUnknown pcmFormat = iota
	formatInt16
	formatInt32
	formatFloat32
)

func pcmFormatFromBits(bits uint16) pcmFormat {
	switch bits {
	case 16:
		return formatInt16
	case 32:
		return formatInt32
	default:
		return formatUnknown
	}
}

// convertFrame converts one interleaved PCM packet (frames frames, each of
// `channels` samples) into a mono float32 frame in dst (len(dst) == frames).
// Samples are normalized to [-1, 1]; mono is the average across channels.
func convertFrame(dst []float32, data *byte, channels int, format pcmFormat) {
	base := unsafe.Pointer(data)
	n := len(dst)
	switch format {
	case formatFloat32:
		words := unsafe.Slice((*float32)(base), n*channels)
		for i := 0; i < n; i++ {
			var sum float32
			for c := 0; c < channels; c++ {
				sum += words[i*channels+c]
			}
			dst[i] = sum / float32(channels)
		}
	case formatInt16:
		words := unsafe.Slice((*int16)(base), n*channels)
		for i := 0; i < n; i++ {
			var sum int32
			for c := 0; c < channels; c++ {
				sum += int32(words[i*channels+c])
			}
			dst[i] = float32(sum) / (float32(channels) * 32768)
		}
	case formatInt32:
		words := unsafe.Slice((*int32)(base), n*channels)
		for i := 0; i < n; i++ {
			var sum int64
			for c := 0; c < channels; c++ {
				sum += int64(words[i*channels+c])
			}
			dst[i] = float32(float64(sum) / (float64(channels) * 2147483648))
		}
	}
}
