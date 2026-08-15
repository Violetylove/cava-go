// Package config loads, validates and generates the TOML configuration
// (see docs/DESIGN.md §6.4).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Config is the runtime configuration, mirroring cava's config sections.
type Config struct {
	General General `toml:"general"`
	DSP     DSP     `toml:"dsp"`
	Smooth  Smooth  `toml:"smoothing"`
	Color   Color   `toml:"color"`
	Keys    Keys    `toml:"keys"`
}

// General holds display-wide settings.
type General struct {
	FPS         int     `toml:"fps"`
	Bars        int     `toml:"bars"`
	AutoSens    bool    `toml:"autosens"`
	Sensitivity float64 `toml:"sensitivity"`
}

// DSP holds the analysis pipeline settings.
type DSP struct {
	FFTSize    int     `toml:"fft_size"`
	Hop        int     `toml:"hop"`
	MinFreq    float64 `toml:"min_freq"`
	MaxFreq    float64 `toml:"max_freq"`
	TargetPeak float64 `toml:"target_peak"`
}

// Smooth holds smoothing settings.
type Smooth struct {
	Falloff    float64 `toml:"falloff"`
	SmoothBars bool    `toml:"smooth_bars"`
}

// Color holds the two-color vertical gradient (bar base -> bar tip).
type Color struct {
	GradientBottom string `toml:"gradient_bottom"` // hex #RRGGBB
	GradientTop    string `toml:"gradient_top"`    // hex #RRGGBB
}

// Keys holds the key bindings (single characters).
type Keys struct {
	Quit     string `toml:"quit"`
	Pause    string `toml:"pause"`
	SensUp   string `toml:"sens_up"`
	SensDown string `toml:"sens_down"`
	Reload   string `toml:"reload"`
}

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		General: General{FPS: 120, Bars: 51, AutoSens: true, Sensitivity: 1.0},
		DSP:     DSP{FFTSize: 2048, Hop: 512, MinFreq: 20, MaxFreq: 20000, TargetPeak: 0.8},
		Smooth:  Smooth{Falloff: 3.0, SmoothBars: true},
		Color:   Color{GradientBottom: "#0000A0", GradientTop: "#00FFFF"},
		Keys:    Keys{Quit: "q", Pause: " ", SensUp: "=", SensDown: "-", Reload: "r"},
	}
}

// DefaultPath returns the default config file location
// (%APPDATA%/cava-go/config.toml).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cava-go", "config.toml"), nil
}

// Load reads a TOML config file. Missing fields fall back to Default.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate checks the configuration for sane values.
func (c *Config) Validate() error {
	if c.General.FPS <= 0 {
		return fmt.Errorf("config: general.fps must be positive, got %d", c.General.FPS)
	}
	if c.General.Bars <= 0 {
		return fmt.Errorf("config: general.bars must be positive, got %d", c.General.Bars)
	}
	if c.DSP.FFTSize <= 0 {
		return fmt.Errorf("config: dsp.fft_size must be positive, got %d", c.DSP.FFTSize)
	}
	if c.DSP.Hop <= 0 || c.DSP.Hop > c.DSP.FFTSize {
		return fmt.Errorf("config: dsp.hop (%d) must be in (0, fft_size=%d]", c.DSP.Hop, c.DSP.FFTSize)
	}
	if c.DSP.MinFreq <= 0 || c.DSP.MaxFreq <= c.DSP.MinFreq {
		return fmt.Errorf("config: dsp frequency range invalid: min=%v max=%v", c.DSP.MinFreq, c.DSP.MaxFreq)
	}
	if c.DSP.TargetPeak <= 0 || c.DSP.TargetPeak > 1 {
		return fmt.Errorf("config: dsp.target_peak must be in (0, 1], got %v", c.DSP.TargetPeak)
	}
	if c.Smooth.Falloff < 0 {
		return fmt.Errorf("config: smoothing.falloff must be >= 0, got %v", c.Smooth.Falloff)
	}
	if c.General.Sensitivity <= 0 {
		return fmt.Errorf("config: general.sensitivity must be positive, got %v", c.General.Sensitivity)
	}
	return nil
}

// defaultConfigText is the commented default configuration written on
// first run. Values must stay in sync with Default() — enforced by
// TestDefaultConfigTextParses.
const defaultConfigText = `# cava-go 配置文件
# 修改后按 r 热重载；结构性参数（bars / fft_size / hop / 频率）变更需重启生效

[general]
# 渲染帧率（帧/秒）
fps = 120
# 柱状条数量（显示时按终端宽度自适应减少）
bars = 51
# 自动增益：让最高柱条接近 target_peak 高度
autosens = true
# 固定灵敏度倍率（autosens 关闭时生效）
sensitivity = 1.0

[dsp]
# FFT 窗口大小（采样点数）
fft_size = 2048
# 相邻 FFT 分析间隔（越小分析越频繁，动画越平滑）
hop = 512
# 分析的最低/最高频率（Hz）
min_freq = 20.0
max_freq = 20000.0
# autosens 目标峰值高度（0~1）
target_peak = 0.8

[smoothing]
# 柱条回落速度（每秒衰减比例，0 为关闭）
falloff = 3.0
# 相邻柱条平滑
smooth_bars = true

[color]
# 渐变颜色（#RRGGBB）：柱条底部 / 顶部
gradient_bottom = '#0000A0'
gradient_top = '#00FFFF'

[keys]
# 快捷键（单字符）
quit = 'q'
pause = ' '
sens_up = '='
sens_down = '-'
reload = 'r'
`

// WriteDefault writes the commented default configuration to path,
// creating directories as needed.
func WriteDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(defaultConfigText), 0o644); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}
