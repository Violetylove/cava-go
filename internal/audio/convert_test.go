package audio

import (
	"math"
	"testing"
	"unsafe"
)

func almostEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-6
}

func TestConvertFrameInt16(t *testing.T) {
	// 2 channels, 2 frames: (1000, 3000) and (-2000, -4000).
	data := []int16{1000, 3000, -2000, -4000}
	dst := make([]float32, 2)
	convertFrame(dst, (*byte)(unsafe.Pointer(&data[0])), 2, formatInt16)
	if want := float32(2000.0 / 32768.0); !almostEqual(dst[0], want) {
		t.Errorf("frame 0 = %v, want %v", dst[0], want)
	}
	if want := float32(-3000.0 / 32768.0); !almostEqual(dst[1], want) {
		t.Errorf("frame 1 = %v, want %v", dst[1], want)
	}
}

func TestConvertFrameInt32(t *testing.T) {
	data := []int32{1 << 30, 1 << 30} // mono (1 channel) full-scale
	dst := make([]float32, 2)
	convertFrame(dst, (*byte)(unsafe.Pointer(&data[0])), 1, formatInt32)
	want := float32(float64(1<<30) / 2147483648.0)
	for i, v := range dst {
		if !almostEqual(v, want) {
			t.Errorf("frame %d = %v, want %v", i, v, want)
		}
	}
}

func TestConvertFrameFloat32(t *testing.T) {
	// 2 channels: (0.5, -0.5) -> mono 0; (0.4, 0.2) -> mono 0.3.
	data := []float32{0.5, -0.5, 0.4, 0.2}
	dst := make([]float32, 2)
	convertFrame(dst, (*byte)(unsafe.Pointer(&data[0])), 2, formatFloat32)
	if !almostEqual(dst[0], 0) || !almostEqual(dst[1], 0.3) {
		t.Errorf("got %v, want [0 0.3]", dst)
	}
}

func TestRMS(t *testing.T) {
	cases := []struct {
		in   []float32
		want float32
	}{
		{nil, 0},
		{[]float32{0, 0, 0}, 0},
		{[]float32{1, -1}, 1},
		{[]float32{3, 4}, float32(math.Sqrt(12.5))}, // sqrt((9+16)/2)
	}
	for i, c := range cases {
		if got := RMS(c.in); !almostEqual(got, c.want) {
			t.Errorf("case %d: RMS = %v, want %v", i, got, c.want)
		}
	}
}
