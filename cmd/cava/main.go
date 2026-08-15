// Command cava-go is a Windows console audio visualizer (cava clone).
//
// Captures system audio (WASAPI loopback), transforms it via FFT into
// spectrum bars and renders them to the terminal with block glyphs.
// Keys: q/Esc/Ctrl-C quit, space pause, +/- sensitivity, r reload config.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"cava-go/internal/audio"
	"cava-go/internal/config"
	"cava-go/internal/dsp"
	"cava-go/internal/render"
)

// version is the build version, overridden at build time via
// -ldflags "-X main.version=<ver>".
var version = "dev"

// fatal prints a human-readable error to stderr and exits. Safe because it
// only runs before the terminal screen becomes active (stderr would corrupt
// the tcell display otherwise — see PROJECT.md 附录 D).
func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "cava-go: %s\n详情: %v\n", msg, err)
	os.Exit(1)
}

func main() {
	configPath := flag.String("config", "", "path to config.toml (default: %APPDATA%/cava-go/config.toml)")
	duration := flag.Duration("duration", 0, "auto-exit after duration (0 = run until quit)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("cava-go %s\n", version)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal("配置文件加载失败（可删除配置文件后重新生成默认配置）", err)
	}

	src := audio.NewWasapiSource()
	frames, err := src.Start()
	if err != nil {
		fatal("音频捕获启动失败（请确认存在可用的音频输出设备且未被其他程序独占）", err)
	}
	defer src.Close()
	sampleRate := src.SampleRate()

	renderer, err := render.New(render.Config{
		FPS:            cfg.General.FPS,
		GradientBottom: cfg.Color.GradientBottom,
		GradientTop:    cfg.Color.GradientTop,
		KeyPause:       keyRune(cfg.Keys.Pause),
		KeySensUp:      keyRune(cfg.Keys.SensUp),
		KeySensDown:    keyRune(cfg.Keys.SensDown),
		KeyReload:      keyRune(cfg.Keys.Reload),
	})
	if err != nil {
		fatal("界面初始化失败（请在 Windows Terminal 或 Windows 10 1809+ 控制台中运行）", err)
	}
	defer renderer.Fini()

	// pipeMu guards pipe so a config reload can swap in a rebuilt pipeline.
	var pipeMu sync.RWMutex
	pipe, err := newPipeline(cfg, sampleRate)
	if err != nil {
		fatal("分析管线初始化失败", err)
	}

	// capture goroutine: audio frames -> DSP pipeline.
	go func() {
		for frame := range frames {
			pipeMu.RLock()
			p := pipe
			pipeMu.RUnlock()
			p.Process(frame)
		}
	}()

	var paused atomic.Bool
	getBars := func() []float32 {
		if paused.Load() {
			return nil // freeze the current frame
		}
		pipeMu.RLock()
		p := pipe
		pipeMu.RUnlock()
		return p.Latest()
	}

	// key actions: pause / sensitivity / config reload.
	// NOTE: never log to stderr here — it shares the terminal with the
	// tcell screen and would corrupt the display.
	actions := make(chan render.KeyAction, 8)
	go func() {
		for a := range actions {
			switch a {
			case render.KeyPause:
				paused.Store(!paused.Load())
			case render.KeySensUp:
				cfg.General.Sensitivity += 0.2
				pipe.SetSensitivity(cfg.General.Sensitivity)
			case render.KeySensDown:
				cfg.General.Sensitivity -= 0.2
				if cfg.General.Sensitivity < 0.2 {
					cfg.General.Sensitivity = 0.2
				}
				pipe.SetSensitivity(cfg.General.Sensitivity)
			case render.KeyReload:
				reload(&cfg, &pipe, &pipeMu, renderer, sampleRate, *configPath)
			}
		}
	}()

	// stop channel: Ctrl-C / duration timer close it (quit keys handled in
	// the render loop itself).
	stop := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		select {
		case <-sig:
		case <-timerOrNever(*duration):
		}
		close(stop)
	}()

	renderer.Run(getBars, actions, stop)
}

// reload re-reads the config, rebuilds the analysis pipeline when
// structural parameters changed, and hot-applies the rest. It never logs:
// stderr shares the terminal with the tcell screen.
func reload(cfg *config.Config, pipe **dsp.Pipeline, pipeMu *sync.RWMutex,
	renderer *render.Renderer, sampleRate int, path string) {
	newCfg, err := loadConfig(path)
	if err != nil {
		return
	}
	structural := newCfg.General.Bars != cfg.General.Bars ||
		newCfg.DSP.FFTSize != cfg.DSP.FFTSize ||
		newCfg.DSP.Hop != cfg.DSP.Hop ||
		newCfg.DSP.MinFreq != cfg.DSP.MinFreq ||
		newCfg.DSP.MaxFreq != cfg.DSP.MaxFreq
	if structural {
		np, err := newPipeline(newCfg, sampleRate)
		if err != nil {
			return
		}
		pipeMu.Lock()
		*pipe = np
		pipeMu.Unlock()
	} else {
		(*pipe).SetSensitivity(newCfg.General.Sensitivity)
	}
	_ = renderer.SetGradient(newCfg.Color.GradientBottom, newCfg.Color.GradientTop)
	renderer.SetFPS(newCfg.General.FPS)
	*cfg = newCfg
}

// newPipeline builds a dsp pipeline from the configuration.
func newPipeline(cfg config.Config, sampleRate int) (*dsp.Pipeline, error) {
	return dsp.New(dsp.Config{
		FFTSize:     cfg.DSP.FFTSize,
		SampleRate:  float64(sampleRate),
		Bars:        cfg.General.Bars,
		MinFreq:     cfg.DSP.MinFreq,
		MaxFreq:     cfg.DSP.MaxFreq,
		Hop:         cfg.DSP.Hop,
		AutoSens:    cfg.General.AutoSens,
		Sensitivity: cfg.General.Sensitivity,
		TargetPeak:  cfg.DSP.TargetPeak,
		Falloff:     cfg.Smooth.Falloff,
		SmoothBars:  cfg.Smooth.SmoothBars,
	})
}

// loadConfig loads the config from path (or the default location), writing
// a generated default config file on first run.
func loadConfig(path string) (config.Config, error) {
	if path == "" {
		def, err := config.DefaultPath()
		if err != nil {
			return config.Config{}, err
		}
		path = def
	}
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if werr := config.WriteDefault(path); werr != nil {
			return config.Config{}, werr
		}
		log.Printf("config: generated default config at %s", path)
		return config.Default(), nil
	}
	return config.Config{}, err
}

// keyRune converts a single-character key binding to a rune (0 = disabled).
func keyRune(s string) rune {
	if s == "" {
		return 0
	}
	return []rune(s)[0]
}

// timerOrNever returns a channel that fires after d, or never when d <= 0.
func timerOrNever(d time.Duration) <-chan time.Time {
	if d <= 0 {
		return make(chan time.Time)
	}
	return time.After(d)
}
