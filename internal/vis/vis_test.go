package vis

import "testing"

func TestRenderSpectrumFullBar(t *testing.T) {
	// One bar at full height over a 2-row grid: every cell is a full block.
	grid := RenderSpectrum([]float32{1.0}, 3, 2)
	if len(grid) != 2 || len(grid[0]) != 3 {
		t.Fatalf("unexpected grid size %dx%d", len(grid), len(grid[0]))
	}
	for _, row := range grid {
		for _, c := range row {
			if c.Rune != runeFull {
				t.Errorf("expected full block, got %q", c.Rune)
			}
			if c.Level != 1.0 {
				t.Errorf("expected level 1, got %v", c.Level)
			}
		}
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
	// width 10, bars 3 → barWidth 3, step 4 → draws 2 bars (8 cols).
	grid := RenderSpectrum([]float32{0, 1, 1}, 10, 2)
	// Second bar starts at col 4 and is full.
	if grid[1][4].Rune != runeFull {
		t.Errorf("bar 1 col should be full, got %q", grid[1][4].Rune)
	}
	if grid[1][6].Rune != runeFull {
		t.Errorf("bar 1 col 6 should be full, got %q", grid[1][6].Rune)
	}
	// Gap column (col 3) must be empty.
	if grid[1][3].Rune != runeEmpty {
		t.Errorf("gap column should be empty, got %q", grid[1][3].Rune)
	}
}

func TestRenderSpectrumNarrow(t *testing.T) {
	// Narrow terminal: only one bar fits.
	grid := RenderSpectrum([]float32{1, 1, 1}, 2, 2)
	if grid[1][0].Rune != runeFull {
		t.Errorf("first bar should render, got %q", grid[1][0].Rune)
	}
	if grid[1][1].Rune != runeEmpty {
		t.Errorf("col 1 should be gap/empty, got %q", grid[1][1].Rune)
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
