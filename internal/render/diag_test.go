package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"cava-go/internal/vis"
)

// TestGapColumnsEmpty guards against a regression where empty cells had
// Fg/Bg zero values, so the renderer painted the gap columns with the
// bottom gradient color (visible as vertical colored slivers while idle).
func TestGapColumnsEmpty(t *testing.T) {
	s := tcell.NewSimulationScreen("utf8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(120, 30)
	s.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorLime))
	s.Clear()

	// Mirror the draw() loop using the renderer's gradient.
	r := newWithScreen(s, Config{FPS: 60})

	// Non-silent bars: every bar at full height.
	bars := make([]float32, 51)
	for i := range bars {
		bars[i] = 1.0
	}
	grid := vis.RenderSpectrum(bars, 120, 30)

	for row := 0; row < 30; row++ {
		for col := 0; col < 120; col++ {
			if grid[row][col].Rune == ' ' && grid[row][col].Fg >= 0 {
				t.Errorf("gap/empty cell %d,%d has Fg=%v (must be -1)", row, col, grid[row][col].Fg)
			}
		}
	}

	// Silence must produce no rendered content at all (no slivers).
	s.Clear()
	for row := 0; row < 30; row++ {
		for col := 0; col < 120; col++ {
			cell := grid[row][col]
			if cell.Fg < 0 {
				continue
			}
			style := tcell.StyleDefault.
				Foreground(gradientColorAt(r.gradient, cell.Fg)).
				Background(gradientColorAt(r.gradient, cell.Bg))
			s.SetContent(col, row, cell.Rune, nil, style)
		}
	}
	s.Show()
	contents, _, _ := s.GetContents()
	nonBlank := 0
	for _, c := range contents {
		if len(c.Bytes) > 0 {
			nonBlank++
		}
	}
	if nonBlank == 0 {
		t.Fatal("full bars should render content")
	}

	s.Clear()
	grid = vis.RenderSpectrum(make([]float32, 51), 120, 30) // silence
	for row := 0; row < 30; row++ {
		for col := 0; col < 120; col++ {
			if grid[row][col].Fg >= 0 {
				t.Errorf("silence: cell %d,%d has Fg=%v", row, col, grid[row][col].Fg)
			}
		}
	}
}
