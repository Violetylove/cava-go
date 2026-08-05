package render

import "testing"

func TestGradientColorAnchors(t *testing.T) {
	cases := []struct {
		level float32
		want  [3]int32
	}{
		{0, [3]int32{0, 0, 255}},
		{0.25, [3]int32{0, 255, 255}},
		{0.5, [3]int32{0, 255, 0}},
		{0.75, [3]int32{255, 255, 0}},
		{1, [3]int32{255, 0, 0}},
		{-1, [3]int32{0, 0, 255}}, // clamped
		{2, [3]int32{255, 0, 0}},  // clamped
	}
	for _, c := range cases {
		r, g, b := gradientColor(c.level).RGB()
		got := [3]int32{r, g, b}
		if got != c.want {
			t.Errorf("level %v: got %v, want %v", c.level, got, c.want)
		}
	}
}

func TestGradientColorInterpolation(t *testing.T) {
	// Midpoint between blue (0,0,255) and cyan (0,255,255).
	r, g, b := gradientColor(0.125).RGB()
	if r != 0 || g != 127 || b != 255 {
		t.Errorf("mid blue-cyan: got (%d,%d,%d), want (0,127,255)", r, g, b)
	}
}
