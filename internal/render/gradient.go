package render

import "github.com/gdamore/tcell/v2"

// gradientStop is a RGB color anchored at a position in [0, 1].
type gradientStop struct {
	pos   float64
	color [3]int32 // R, G, B
}

// spectrumGradient is cava's default "gradient 1" palette.
var spectrumGradient = []gradientStop{
	{0.00, [3]int32{0, 0, 255}},   // blue
	{0.25, [3]int32{0, 255, 255}}, // cyan
	{0.50, [3]int32{0, 255, 0}},   // green
	{0.75, [3]int32{255, 255, 0}}, // yellow
	{1.00, [3]int32{255, 0, 0}},   // red
}

// gradientColor maps a bar level in [0, 1] to a truecolor by linear RGB
// interpolation between the gradient stops.
func gradientColor(level float32) tcell.Color {
	l := float64(level)
	if l <= 0 {
		return rgbColor(spectrumGradient[0].color)
	}
	if l >= 1 {
		return rgbColor(spectrumGradient[len(spectrumGradient)-1].color)
	}
	for i := 1; i < len(spectrumGradient); i++ {
		if l <= spectrumGradient[i].pos {
			lo, hi := spectrumGradient[i-1], spectrumGradient[i]
			t := (l - lo.pos) / (hi.pos - lo.pos)
			var rgb [3]int32
			for c := 0; c < 3; c++ {
				rgb[c] = lo.color[c] + int32(float64(hi.color[c]-lo.color[c])*t)
			}
			return rgbColor(rgb)
		}
	}
	return rgbColor(spectrumGradient[len(spectrumGradient)-1].color)
}

func rgbColor(rgb [3]int32) tcell.Color {
	return tcell.NewRGBColor(rgb[0], rgb[1], rgb[2])
}
