package render

import "testing"

func TestGradientColorAnchors(t *testing.T) {
	g, err := buildGradient("#0000A0", "#00FFFF")
	if err != nil {
		t.Fatal(err)
	}
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
		r, g, b := gradientColorAt(g, c.level).RGB()
		got := [3]int32{r, g, b}
		if got != c.want {
			t.Errorf("level %v: got %v, want %v", c.level, got, c.want)
		}
	}
}

func TestGradientColorInterpolation(t *testing.T) {
	g, err := buildGradient("#0000A0", "#00FFFF")
	if err != nil {
		t.Fatal(err)
	}
	// Midpoint between deep blue (0,0,160) and bright cyan (0,255,255).
	r, g2, b := gradientColorAt(g, 0.5).RGB()
	if r != 0 || g2 != 127 || b != 207 {
		t.Errorf("mid gradient: got (%d,%d,%d), want (0,127,207)", r, g2, b)
	}
}

func TestParseHexColor(t *testing.T) {
	c, err := parseHexColor("#1E90FF")
	if err != nil {
		t.Fatal(err)
	}
	if c != [3]int32{30, 144, 255} {
		t.Errorf("got %v, want (30,144,255)", c)
	}
	if _, err := parseHexColor("zzz"); err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestBuildGradientInvalid(t *testing.T) {
	if _, err := buildGradient("nope", "#00FFFF"); err == nil {
		t.Error("expected error for invalid color")
	}
}
