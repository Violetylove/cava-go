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
	// Level in [0, 1] is the bar height at this cell, used for color
	// gradients. Negative means no bar content.
	Level float32
}

// Block glyphs. Each terminal row spans two "half-blocks" vertically,
// giving twice the vertical resolution of the character grid.
const (
	runeEmpty  = ' '
	runeTop    = '▀' // top half filled
	runeBottom = '▄' // bottom half filled
	runeFull   = '█' // both halves filled
)

// RenderSpectrum lays out bars as block-glyph columns over a width×height
// character grid (height rows, each 2 half-blocks tall). Bars are drawn
// bottom-up. Each bar gets an equal share of the terminal width (no gap),
// clamped to [1, 6] columns — adjacent bars make them visibly thicker.
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

	barWidth := width / len(bars)
	if barWidth < 1 {
		barWidth = 1
	}
	if barWidth > 6 {
		barWidth = 6
	}
	step := barWidth

	drawable := width / step
	if drawable > len(bars) {
		drawable = len(bars)
	}
	if drawable < 1 {
		drawable = 1
	}

	for b := 0; b < drawable; b++ {
		v := bars[b]
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		half := int(math.Round(float64(v) * float64(height) * 2)) // filled half-blocks

		colStart := b * step
		for c := 0; c < barWidth; c++ {
			col := colStart + c
			if col >= width {
				break
			}
			for row := 0; row < height; row++ {
				// Half-block indices of this row, counted from the bottom.
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
				grid[row][col] = Cell{Rune: rn, Level: v}
			}
		}
	}
	return grid
}
