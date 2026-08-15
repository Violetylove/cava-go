package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.General.FPS != 120 || c.General.Bars != 51 || !c.General.AutoSens {
		t.Errorf("unexpected general defaults: %+v", c.General)
	}
	if c.Smooth.Falloff != 3.0 || !c.Smooth.SmoothBars {
		t.Errorf("unexpected smoothing defaults: %+v", c.Smooth)
	}
	if c.Color.GradientBottom != "#0000A0" || c.Color.GradientTop != "#00FFFF" {
		t.Errorf("unexpected color defaults: %+v", c.Color)
	}
	if c.Keys.Pause != " " || c.Keys.Quit != "q" || c.Keys.SensUp != "=" || c.Keys.SensDown != "-" {
		t.Errorf("unexpected key defaults: %+v", c.Keys)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOverridesAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[general]\nfps = 60\nbars = 32\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.General.FPS != 60 || c.General.Bars != 32 {
		t.Errorf("overrides not applied: %+v", c.General)
	}
	// Unset fields fall back to defaults.
	if c.Smooth.Falloff != 3.0 || c.Color.GradientTop != "#00FFFF" {
		t.Errorf("defaults not applied: %+v / %+v", c.Smooth, c.Color)
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("[general]\nfps = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected validation error for fps=0")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestWriteDefaultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.General.FPS != 120 || c.DSP.Hop != 512 {
		t.Errorf("round-trip mismatch: %+v / %+v", c.General, c.DSP)
	}
}
