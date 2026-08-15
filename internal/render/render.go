// Package render implements terminal output via tcell: block-glyph
// spectrum drawing, fps pacing, key handling and quit. See docs/DESIGN.md §6.
package render

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"cava-go/internal/vis"
)

// Config configures the terminal renderer.
type Config struct {
	FPS            int    // target frames per second (default 60)
	GradientBottom string // hex #RRGGBB (bar base), default "#0000A0"
	GradientTop    string // hex #RRGGBB (bar tip), default "#00FFFF"

	// Key bindings (single runes) reported via the Run actions channel.
	KeyPause    rune
	KeySensUp   rune
	KeySensDown rune
	KeyReload   rune
}

// KeyAction is a non-quit key press reported to the caller.
type KeyAction int

const (
	// KeyPause toggles freezing the display (space).
	KeyPause KeyAction = iota
	// KeySensUp raises the sensitivity (+).
	KeySensUp
	// KeySensDown lowers the sensitivity (-).
	KeySensDown
	// KeyReload reloads the configuration (r).
	KeyReload
)

// Renderer draws visualizations to the terminal via tcell.
type Renderer struct {
	screen    tcell.Screen
	cfg       Config
	maxMaxBar float32 // highest single-frame max bar seen (diagnostics)

	mu       sync.Mutex
	gradient []gradientStop
	fps      atomic.Int32

	// dirty-rect state: which cells were drawn last frame. Only changed
	// cells are touched, so tcell's Show emits just the diffs instead of
	// the whole screen every frame (Clear marks everything dirty).
	prevW, prevH int
	prevDrawn    []bool
}

// New initializes the terminal (alternate screen buffer, hidden cursor).
// Call Fini before exiting the program.
func New(cfg Config) (*Renderer, error) {
	if cfg.FPS <= 0 {
		cfg.FPS = 60
	}
	if _, err := buildGradient(cfg.GradientBottom, cfg.GradientTop); err != nil {
		return nil, err
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
	return newWithScreen(s, cfg), nil
}

// newWithScreen builds a renderer around an existing screen (used by tests
// with a SimulationScreen). Invalid gradient colors fall back to defaults.
func newWithScreen(s tcell.Screen, cfg Config) *Renderer {
	if cfg.FPS <= 0 {
		cfg.FPS = 60
	}
	r := &Renderer{screen: s, cfg: cfg}
	g, err := buildGradient(cfg.GradientBottom, cfg.GradientTop)
	if err != nil || g == nil {
		g = defaultGradient
	}
	r.gradient = g
	r.fps.Store(int32(cfg.FPS))
	return r
}

// Fini restores the terminal and releases resources.
func (r *Renderer) Fini() {
	r.screen.Fini()
}

// SetGradient updates the two-color gradient at runtime (hot reload).
func (r *Renderer) SetGradient(bottom, top string) error {
	g, err := buildGradient(bottom, top)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.gradient = g
	r.mu.Unlock()
	return nil
}

// SetFPS updates the target frame rate at runtime (hot reload).
func (r *Renderer) SetFPS(fps int) {
	if fps <= 0 {
		fps = 60
	}
	r.fps.Store(int32(fps))
}

// Run drives the render loop until the stop channel is closed or the user
// presses the quit key. getBars is polled every tick for the newest bar
// frame (return nil to freeze the current frame, e.g. when paused).
// Non-quit key presses are sent to actions (may be nil).
func (r *Renderer) Run(getBars func() []float32, actions chan<- KeyAction, stop <-chan struct{}) {
	events := make(chan tcell.Event, 32)
	go func() {
		for {
			events <- r.screen.PollEvent()
		}
	}()

	timer := time.NewTimer(time.Second / time.Duration(r.fps.Load()))
	defer timer.Stop()

	start := time.Now()
	frames := 0
	var maxSum float32
	for {
		select {
		case <-timer.C:
			mb := r.draw(getBars())
			if mb > r.maxMaxBar {
				r.maxMaxBar = mb
			}
			maxSum += mb
			r.screen.Show()
			frames++
			timer.Reset(time.Second / time.Duration(r.fps.Load()))
		case ev := <-events:
			if key, ok := ev.(*tcell.EventKey); ok {
				switch {
				case key.Key() == tcell.KeyEscape || key.Rune() == 'q' || key.Rune() == 'Q' ||
					key.Key() == tcell.KeyCtrlC:
					r.report(frames, maxSum, start)
					return
				case key.Rune() == r.cfg.KeyPause:
					sendAction(actions, KeyPause)
				case key.Rune() == r.cfg.KeySensUp:
					sendAction(actions, KeySensUp)
				case key.Rune() == r.cfg.KeySensDown:
					sendAction(actions, KeySensDown)
				case key.Rune() == r.cfg.KeyReload:
					sendAction(actions, KeyReload)
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

func sendAction(actions chan<- KeyAction, a KeyAction) {
	if actions == nil {
		return
	}
	select {
	case actions <- a:
	default: // never block the render loop
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

// draw renders the latest bar frame with dirty-rect updates and returns
// the tallest visible bar (for statistics).
func (r *Renderer) draw(bars []float32) float32 {
	width, height := r.screen.Size()
	if len(bars) == 0 || width <= 0 || height <= 0 {
		return 0
	}
	// Full redraw on resize or first frame.
	if width != r.prevW || height != r.prevH || r.prevDrawn == nil {
		r.screen.Clear()
		r.prevDrawn = make([]bool, width*height)
		r.prevW, r.prevH = width, height
	}
	grid := vis.RenderSpectrum(bars, width, height)
	maxBar := float32(0)
	for _, v := range bars {
		if v > maxBar {
			maxBar = v
		}
	}
	r.mu.Lock()
	g := r.gradient
	r.mu.Unlock()
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			idx := row*width + col
			cell := grid[row][col]
			if cell.Fg >= 0 {
				// Foreground shades the filled half-block, background the
				// other half — continuous two-tone gradient.
				style := tcell.StyleDefault.
					Foreground(gradientColorAt(g, cell.Fg)).
					Background(gradientColorAt(g, cell.Bg))
				r.screen.SetContent(col, row, cell.Rune, nil, style)
				r.prevDrawn[idx] = true
			} else if r.prevDrawn[idx] {
				// Erase cells that were drawn last frame but are empty now.
				r.screen.SetContent(col, row, ' ', nil, tcell.StyleDefault)
				r.prevDrawn[idx] = false
			}
		}
	}
	return maxBar
}
