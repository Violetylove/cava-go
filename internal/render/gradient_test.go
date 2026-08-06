package render

import "testing"

func TestGradientColorAnchors(t *testing.T) {
	cases := []struct {
		level float32
		want  [3]int32
	}{
		{0, [3]int32{0, 0, 160}},   // deep blue
		{1, [3]int32{0, 255, 255}}, // bright cyan
		{-1, [3]int32{0, 0, 160}},  // clamped
		{2, [3]int32{0, 255, 255}}, // clamped
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
	// Midpoint between deep blue (0,0,160) and bright cyan (0,255,255).
	r, g, b := gradientColor(0.5).RGB()
	if r != 0 || g != 127 || b != 207 {
		t.Errorf("mid gradient: got (%d,%d,%d), want (0,127,207)", r, g, b)
	}
}
