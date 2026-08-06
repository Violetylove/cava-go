package vis

import "testing"

func TestRenderSpectrumFullBar(t *testing.T) {
	// One bar at full height over a 2-row grid: every cell is a full block.
	// Level runs from 1 (top row) down to 0 (bottom row).
	grid := RenderSpectrum([]float32{1.0}, 3, 2)
	if len(grid) != 2 || len(grid[0]) != 3 {
		t.Fatalf("unexpected grid size %dx%d", len(grid), len(grid[0]))
	}
	for row, cells := range grid {
		for _, c := range cells {
			if c.Rune != runeFull {
				t.Errorf("row %d: expected full block, got %q", row, c.Rune)
			}
		}
	}
	if grid[0][0].Level != 1 {
		t.Errorf("top row level = %v, want 1", grid[0][0].Level)
	}
	if grid[1][0].Level != 0 {
		t.Errorf("bottom row level = %v, want 0", grid[1][0].Level)
	}
}

func TestRenderSpectrumHalfBar(t *testing.T) {
	// Height 2 (4 half-blocks), bar at 0.5 → 2 half-blocks filled:
	// bottom row full block, top row empty.
	grid := RenderSpectrum([]float32{0.5}, 3, 2)
	if grid[1][0].Rune != runeFull {
		t.Errorf("bottom row should be full, got %q", grid[1][0].Rune)
	}
	if grid[0][0].Rune != runeEmpty {
		t.Errorf("top row should be empty, got %q", grid[0][0].Rune)
	}
}

func TestRenderSpectrumQuarterBar(t *testing.T) {
	// Height 1 (2 half-blocks), bar at 0.25 → 0.5 half-block rounds to 1:
	// bottom half filled (▄).
	grid := RenderSpectrum([]float32{0.25}, 1, 1)
	if grid[0][0].Rune != runeBottom {
		t.Errorf("expected bottom-half block, got %q", grid[0][0].Rune)
	}
}

func TestRenderSpectrumLayout(t *testing.T) {
	// width 10, bars 3 → barWidth 3; with a 1-col gap the set overflows
	// (12 > 10), so the bar count is reduced to 2 and bars are sampled
	// evenly: indices 0 and 2. Bar 0 (value 0) at cols 0-2, gap col 3,
	// bar 2 (value 1) at cols 4-6.
	grid := RenderSpectrum([]float32{0, 1, 1}, 10, 2)
	if grid[1][4].Rune != runeFull || grid[1][6].Rune != runeFull {
		t.Errorf("bar at cols 4-6 should be full, got %q %q", grid[1][4].Rune, grid[1][6].Rune)
	}
	if grid[1][3].Rune != runeEmpty {
		t.Errorf("gap column should be empty, got %q", grid[1][3].Rune)
	}
	// First bar (value 0) at cols 0-2 stays empty.
	if grid[1][0].Rune != runeEmpty {
		t.Errorf("empty bar col 0 should be empty, got %q", grid[1][0].Rune)
	}
}

func TestRenderSpectrumNarrow(t *testing.T) {
	// width 2, bars 3 → barWidth 1, gap 1: only one bar fits (drawable 1).
	grid := RenderSpectrum([]float32{1, 1, 1}, 2, 2)
	if grid[1][0].Rune != runeFull {
		t.Errorf("first bar should render, got %q", grid[1][0].Rune)
	}
	if grid[1][1].Rune != runeEmpty {
		t.Errorf("gap column should be empty, got %q", grid[1][1].Rune)
	}
}

func TestRenderSpectrumEmpty(t *testing.T) {
	if RenderSpectrum(nil, 10, 5) != nil {
		t.Error("expected nil grid for empty bars")
	}
	if RenderSpectrum([]float32{0.5}, 0, 5) != nil {
		t.Error("expected nil grid for zero width")
	}
}
