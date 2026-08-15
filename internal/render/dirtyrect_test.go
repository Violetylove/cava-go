package render

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDirtyRectErase verifies that cells drawn in one frame are erased when
// the next frame is empty (dirty-rect rendering must not leave residue).
func TestDirtyRectErase(t *testing.T) {
	s := tcell.NewSimulationScreen("utf8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(40, 10)
	s.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorLime))

	r := newWithScreen(s, Config{FPS: 60})

	countContent := func() int {
		s.Show()
		contents, _, _ := s.GetContents()
		n := 0
		for _, c := range contents {
			if string(c.Bytes) != " " && len(c.Bytes) > 0 {
				n++
			}
		}
		return n
	}

	// Frame 1: full bars → content drawn.
	bars := make([]float32, 10)
	for i := range bars {
		bars[i] = 1.0
	}
	r.draw(bars)
	if n := countContent(); n == 0 {
		t.Fatal("frame 1 should draw content")
	}

	// Frame 2: silence → everything erased, nothing remains.
	r.draw(make([]float32, 10))
	if n := countContent(); n != 0 {
		t.Errorf("silence frame left %d cells with content", n)
	}
	for i, d := range r.prevDrawn {
		if d {
			t.Errorf("prevDrawn[%d] should be false after erase", i)
		}
	}

	// Frame 3: bars return → content is drawn again (no stale erase state).
	r.draw(bars)
	if n := countContent(); n == 0 {
		t.Fatal("frame 3 should draw content again")
	}
}
