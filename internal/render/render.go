// Package render implements terminal output via tcell: block-glyph
// spectrum drawing, fps pacing and quit handling. See docs/DESIGN.md §6.
package render

import (
	"log"
	"time"

	"github.com/gdamore/tcell/v2"

	"cava-go/internal/vis"
)

// Config configures the terminal renderer.
type Config struct {
	FPS int // target frames per second (default 30)
}

// Renderer draws visualizations to the terminal via tcell.
type Renderer struct {
	screen tcell.Screen
	cfg    Config
}

// New initializes the terminal (alternate screen buffer, hidden cursor).
// Call Fini before exiting the program.
func New(cfg Config) (*Renderer, error) {
	if cfg.FPS <= 0 {
		cfg.FPS = 30
	}
	s, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	s.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorLime).Background(tcell.ColorBlack))
	s.Clear()
	return &Renderer{screen: s, cfg: cfg}, nil
}

// Fini restores the terminal and releases resources.
func (r *Renderer) Fini() {
	r.screen.Fini()
}

// Run drives the render loop until the stop channel is closed or the user
// presses q / Esc / Ctrl-C. getBars is polled every tick for the newest
// bar frame (returns nil when no data is available yet).
func (r *Renderer) Run(getBars func() []float32, stop <-chan struct{}) {
	events := make(chan tcell.Event, 32)
	go func() {
		for {
			events <- r.screen.PollEvent()
		}
	}()

	ticker := time.NewTicker(time.Second / time.Duration(r.cfg.FPS))
	defer ticker.Stop()

	start := time.Now()
	frames := 0
	for {
		select {
		case <-ticker.C:
			r.draw(getBars())
			r.screen.Show()
			frames++
		case ev := <-events:
			if key, ok := ev.(*tcell.EventKey); ok {
				if key.Key() == tcell.KeyEscape || key.Rune() == 'q' || key.Rune() == 'Q' ||
					key.Key() == tcell.KeyCtrlC {
					elapsed := time.Since(start).Seconds()
					if elapsed > 0 {
						log.Printf("render: %d frames in %.1fs (%.1f fps)", frames, elapsed, float64(frames)/elapsed)
					}
					return
				}
			}
			// EventResize and others: handled implicitly on the next draw
			// via screen.Size().
		case <-stop:
			elapsed := time.Since(start).Seconds()
			if elapsed > 0 {
				log.Printf("render: %d frames in %.1fs (%.1f fps)", frames, elapsed, float64(frames)/elapsed)
			}
			return
		}
	}
}

// draw renders the latest bar frame to the terminal.
func (r *Renderer) draw(bars []float32) {
	width, height := r.screen.Size()
	if len(bars) == 0 || width <= 0 || height <= 0 {
		return
	}
	grid := vis.RenderSpectrum(bars, width, height)
	r.screen.Clear()
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			cell := grid[row][col]
			if cell.Rune == ' ' {
				continue
			}
			// M2: single color; color gradients land in M3.
			style := tcell.StyleDefault.Foreground(tcell.ColorLime)
			r.screen.SetContent(col, row, cell.Rune, nil, style)
		}
	}
}
