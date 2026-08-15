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
	screen    tcell.Screen
	cfg       Config
	maxMaxBar float32 // highest single-frame max bar seen (diagnostics)
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
	// Keep the terminal's own background (no forced black): the theme's
	// background color shows through in empty areas.
	s.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorLime))
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
	var maxSum float32
	for {
		select {
		case <-ticker.C:
			mb := r.draw(getBars())
			if mb > r.maxMaxBar {
				r.maxMaxBar = mb
			}
			maxSum += mb
			r.screen.Show()
			frames++
		case ev := <-events:
			if key, ok := ev.(*tcell.EventKey); ok {
				if key.Key() == tcell.KeyEscape || key.Rune() == 'q' || key.Rune() == 'Q' ||
					key.Key() == tcell.KeyCtrlC {
					r.report(frames, maxSum, start)
					return
				}
			}
			// EventResize and others: handled implicitly on the next draw
			// via screen.Size().
		case <-stop:
			r.report(frames, maxSum, start)
			return
		}
	}
}

// report prints render statistics to stderr (does not touch the screen).
func (r *Renderer) report(frames int, maxSum float32, start time.Time) {
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 || frames == 0 {
		log.Printf("render: %d frames in %.1fs", frames, elapsed)
		return
	}
	log.Printf("render: %d frames in %.1fs (%.1f fps), avg max bar = %.2f, peak max bar = %.2f",
		frames, elapsed, float64(frames)/elapsed, maxSum/float32(frames), r.maxMaxBar)
}

// draw renders the latest bar frame and returns the tallest visible bar
// (for statistics).
func (r *Renderer) draw(bars []float32) float32 {
	width, height := r.screen.Size()
	if len(bars) == 0 || width <= 0 || height <= 0 {
		return 0
	}
	grid := vis.RenderSpectrum(bars, width, height)
	r.screen.Clear()
	maxBar := float32(0)
	for _, v := range bars {
		if v > maxBar {
			maxBar = v
		}
	}
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			cell := grid[row][col]
			if cell.Fg < 0 {
				continue
			}
			// Foreground shades the filled half-block, background the other
			// half — continuous two-tone gradient, no dark line on tops.
			style := tcell.StyleDefault.
				Foreground(gradientColor(cell.Fg)).
				Background(gradientColor(cell.Bg))
			r.screen.SetContent(col, row, cell.Rune, nil, style)
		}
	}
	return maxBar
}
