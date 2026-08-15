package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// gradientStop is a RGB color anchored at a position in [0, 1].
type gradientStop struct {
	pos   float64
	color [3]int32 // R, G, B
}

// defaultGradient is the fallback two-color gradient (deep blue -> cyan).
var defaultGradient = func() []gradientStop {
	g, _ := buildGradient("#0000A0", "#00FFFF")
	return g
}()

// buildGradient builds a two-stop gradient from hex colors (#RRGGBB).
func buildGradient(bottom, top string) ([]gradientStop, error) {
	b, err := parseHexColor(bottom)
	if err != nil {
		return nil, fmt.Errorf("render: bottom color: %w", err)
	}
	t, err := parseHexColor(top)
	if err != nil {
		return nil, fmt.Errorf("render: top color: %w", err)
	}
	return []gradientStop{
		{0.0, b},
		{1.0, t},
	}, nil
}

// parseHexColor parses "#RRGGBB" into an RGB triple.
func parseHexColor(s string) ([3]int32, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return [3]int32{}, fmt.Errorf("invalid hex color %q (want #RRGGBB)", s)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return [3]int32{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	return [3]int32{int32(v >> 16 & 0xFF), int32(v >> 8 & 0xFF), int32(v & 0xFF)}, nil
}

// gradientColorAt maps a level in [0, 1] to a truecolor by linear RGB
// interpolation between the gradient stops.
func gradientColorAt(g []gradientStop, level float32) tcell.Color {
	l := float64(level)
	if l <= 0 {
		return rgbColor(g[0].color)
	}
	if l >= 1 {
		return rgbColor(g[len(g)-1].color)
	}
	for i := 1; i < len(g); i++ {
		if l <= g[i].pos {
			lo, hi := g[i-1], g[i]
			t := (l - lo.pos) / (hi.pos - lo.pos)
			var rgb [3]int32
			for c := 0; c < 3; c++ {
				rgb[c] = lo.color[c] + int32(float64(hi.color[c]-lo.color[c])*t)
			}
			return rgbColor(rgb)
		}
	}
	return rgbColor(g[len(g)-1].color)
}

func rgbColor(rgb [3]int32) tcell.Color {
	return tcell.NewRGBColor(rgb[0], rgb[1], rgb[2])
}
