package vis

import "testing"

func TestRenderSpectrumFullBar(t *testing.T) {
	// One bar at full height over a 2-row grid: every cell is a full block
	// (flat top, cava-style). Level runs 1 (top) to 0 (bottom).
	grid := RenderSpectrum([]float32{1.0}, 3, 2)
	if len(grid) != 2 || len(grid[0]) != 3 {
		t.Fatalf("unexpected grid size %dx%d", len(grid), len(grid[0]))
	}
	for _, cells := range grid {
		for _, c := range cells {
			if c.Rune != runeFull {
				t.Errorf("expected full blocks, got %q", c.Rune)
			}
		}
	}
	if grid[0][0].Fg != 1 {
		t.Errorf("top row upper half level = %v, want 1", grid[0][0].Fg)
	}
	if grid[1][0].Bg != 0 {
		t.Errorf("bottom row lower half level = %v, want 0", grid[1][0].Bg)
	}
}

func TestRenderSpectrumHalfBar(t *testing.T) {
	// Height 2 (4 half-blocks), bar at 0.5 → 2 half-blocks filled:
	// bottom row full blocks, top row empty.
	grid := RenderSpectrum([]float32{0.5}, 3, 2)
	if grid[1][0].Rune != runeFull {
		t.Errorf("bottom row should be full, got %q", grid[1][0].Rune)
	}
	if grid[0][0].Rune != runeEmpty {
		t.Errorf("top row should be empty, got %q", grid[0][0].Rune)
	}
}

func TestRenderSpectrumQuarterBar(t *testing.T) {
	// Height 2 (4 half-blocks), bar at 0.3 → 1.2 half-blocks rounds to 1:
	// a single lower half-block (▄) on the bottom row.
	grid := RenderSpectrum([]float32{0.3}, 2, 2)
	if grid[1][0].Rune != runeBottom {
		t.Errorf("expected bottom-half block, got %q", grid[1][0].Rune)
	}
	if grid[0][0].Rune != runeEmpty {
		t.Errorf("top row should be empty, got %q", grid[0][0].Rune)
	}
}

func TestRenderSpectrumDeadZone(t *testing.T) {
	// Near-silence values (below one half-block) must draw nothing,
	// otherwise autosens-amplified noise shows as 1px colored dots.
	grid := RenderSpectrum([]float32{0.005}, 5, 30)
	for _, row := range grid {
		for _, c := range row {
			if c.Fg >= 0 {
				t.Fatalf("expected empty grid for dead-zone value, got rune %q", c.Rune)
			}
		}
	}
}

func TestRenderSpectrumLayout(t *testing.T) {
	// width 10, bars 3 → barWidth 4 (+1 for thickness); with a 1-col gap
	// the set overflows (15 > 10), so the bar count is reduced to 2 and
	// bars are sampled evenly: indices 0 and 2. Bar 0 (value 0) at cols
	// 0-3, gap col 4, bar 2 (value 1) at cols 5-8.
	grid := RenderSpectrum([]float32{0, 1, 1}, 10, 2)
	if grid[1][5].Rune != runeFull || grid[1][8].Rune != runeFull {
		t.Errorf("bar at cols 5-8 should be full, got %q %q", grid[1][5].Rune, grid[1][8].Rune)
	}
	if grid[1][4].Rune != runeEmpty {
		t.Errorf("gap column should be empty, got %q", grid[1][4].Rune)
	}
	// First bar (value 0) at cols 0-3 stays empty.
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
