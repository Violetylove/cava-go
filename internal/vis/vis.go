// Package vis defines visualization data contracts and drawable
// visualizations (spectrum, waveform, ...). See docs/DESIGN.md §5.5 and §6.1.
package vis

import "math"

// Frame is the per-frame data consumed by visualizations.
type Frame struct {
	// Bars holds bar heights in [0, 1] for spectrum-based visuals.
	Bars []float32

	// Wave holds normalized time-domain samples for waveform visuals.
	Wave []float32
}

// Cell is one terminal cell produced by a visualization.
type Cell struct {
	// Rune is the glyph to draw; ' ' means empty.
	Rune rune
	// Fg is the gradient level (0 bottom .. 1 top) shading the FILLED
	// half-block (the glyph's foreground color).
	// Bg is the gradient level shading the OTHER half-block area (the
	// glyph's background). Shading it with the gradient instead of the
	// terminal background removes the dark line on half-block bar tops and
	// doubles the vertical color resolution for a smooth gradient.
	// Negative values mean "no content" (cell is empty).
	Fg float32
	Bg float32
}

// Block glyphs. Each terminal row spans two "half-blocks" vertically,
// giving twice the vertical resolution of the character grid.
//
// Bars are deliberately flat-topped (cava-style). True rounded corners are
// impossible in a character grid (the smallest unit is a quadrant block
// with right angles); some terminals render these block glyphs rounded
// themselves (e.g. kitty's block_shape=rounded), which Windows Terminal
// does not support — so flat tops are the compatible choice.
const (
	runeEmpty  = ' '
	runeTop    = '▀' // top half filled
	runeBottom = '▄' // bottom half filled
	runeFull   = '█' // both halves filled
)

// RenderSpectrum lays out bars as block-glyph columns over a width×height
// character grid (height rows, each 2 half-blocks tall). Bars are drawn
// bottom-up. Layout: each bar gets barWidth columns plus a 1-column gap;
// if the full set would overflow the canvas, the number of bars is reduced
// (sampled evenly across the spectrum) instead of squeezing or truncating.
// Cell.Level is the row's vertical position within its bar (0 = bottom,
// 1 = top), used for the per-bar vertical color gradient.
func RenderSpectrum(bars []float32, width, height int) [][]Cell {
	if height <= 0 || width <= 0 || len(bars) == 0 {
		return nil
	}

	grid := make([][]Cell, height)
	for r := range grid {
		grid[r] = make([]Cell, width)
		for c := range grid[r] {
			grid[r][c].Rune = runeEmpty
		}
	}

	barWidth := width/len(bars) + 1 // one extra column per bar for thickness
	if barWidth < 1 {
		barWidth = 1
	}
	if barWidth > 8 {
		barWidth = 8
	}
	const gap = 1

	drawable := len(bars)
	if drawable*(barWidth+gap) > width {
		drawable = width / (barWidth + gap)
		if drawable < 1 {
			drawable = 1
		}
	}

	for i := 0; i < drawable; i++ {
		// Evenly sample bars across the full spectrum when reduced.
		idx := 0
		if drawable > 1 {
			idx = i * (len(bars) - 1) / (drawable - 1)
		}
		v := bars[idx]
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		// Dead zone: values below one half-block are dropped instead of
		// rounding up to a 1px sliver — near-silence noise amplified by the
		// autosens gain would otherwise draw a colored dot on every bar
		// position while idle.
		half := 0
		if float64(v)*float64(height)*2 >= 1 {
			half = int(math.Round(float64(v) * float64(height) * 2))
		}

		colStart := i * (barWidth + gap)
		// Actual visible columns of this bar (clipped by the canvas width).
		visible := barWidth
		if width-colStart < visible {
			visible = width - colStart
		}

		for c := 0; c < visible; c++ {
			col := colStart + c
			for row := 0; row < height; row++ {
				// Half-block indices of this row, counted from the bottom
				// (0 = bottommost half-block, 2*height-1 = topmost).
				bottom := 2 * (height - 1 - row)
				top := bottom + 1
				var rn rune
				switch {
				case bottom < half && top < half:
					rn = runeFull
				case top < half:
					rn = runeTop
				case bottom < half:
					rn = runeBottom
				default:
					rn = runeEmpty
				}
				if rn == runeEmpty {
					grid[row][col] = Cell{Rune: runeEmpty, Fg: -1, Bg: -1}
					continue
				}
				maxIdx := float32(2*height - 1)
				loLevel := float32(bottom) / maxIdx
				hiLevel := float32(top) / maxIdx
				// Fg shades the filled half-block, Bg the other half so the
				// bar reads as one continuous vertical gradient.
				switch rn {
				case runeTop:
					grid[row][col] = Cell{Rune: rn, Fg: hiLevel, Bg: loLevel}
				case runeBottom:
					grid[row][col] = Cell{Rune: rn, Fg: loLevel, Bg: hiLevel}
				default:
					grid[row][col] = Cell{Rune: rn, Fg: hiLevel, Bg: loLevel}
				}
			}
		}
	}
	return grid
}
