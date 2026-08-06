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

	// Rounded bar-top glyphs (1px corner radius), drawn on the row that
	// contains the highest filled half-block:
	//  - upper-half top: left corner lacks top-right quadrant (▜), right
	//    corner lacks top-left quadrant (▟);
	//  - lower-half top: left/lower-left (▖) and right/lower-right (▗).
	runeRoundUpLeft  = '▜'
	runeRoundUpRight = '▟'
	runeRoundLoLeft  = '▖'
	runeRoundLoRight = '▗'
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
		half := int(math.Round(float64(v) * float64(height) * 2)) // filled half-blocks

		colStart := i * (barWidth + gap)
		// Actual visible columns of this bar (clipped by the canvas width).
		visible := barWidth
		if width-colStart < visible {
			visible = width - colStart
		}
		// Rounded top: the row holding the highest filled half-block draws
		// corner glyphs on its first/last visible columns (needs ≥2 visible
		// columns; single-column bars stay flat).
		rowTop := -1
		topIsUpper := false
		if half > 0 && visible >= 2 {
			t := half - 1
			rowTop = height - 1 - t/2
			topIsUpper = t%2 == 1
		}

		for c := 0; c < visible; c++ {
			col := colStart + c
			for row := 0; row < height; row++ {
				// Half-block indices of this row, counted from the bottom.
				bottom := 2 * (height - 1 - row)
				top := bottom + 1
				var rn rune
				if row == rowTop {
					switch {
					case c == 0:
						if topIsUpper {
							rn = runeRoundUpLeft
						} else {
							rn = runeRoundLoLeft
						}
					case c == visible-1:
						if topIsUpper {
							rn = runeRoundUpRight
						} else {
							rn = runeRoundLoRight
						}
					default:
						if topIsUpper {
							rn = runeFull
						} else {
							rn = runeBottom
						}
					}
				} else {
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
				}
				level := float32(1) // vertical position: 0 bottom .. 1 top
				if height > 1 {
					level = 1 - float32(row)/float32(height-1)
				}
				grid[row][col] = Cell{Rune: rn, Level: level}
			}
		}
	}
	return grid
}
