package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultPollInterval(t *testing.T) {
	cfg := Default()
	if cfg.PollInterval != 120*time.Second {
		t.Fatalf("Default().PollInterval = %v, want %v", cfg.PollInterval, 120*time.Second)
	}
}

func TestParseLogPathFromEnv(t *testing.T) {
	t.Setenv("GOGAP_LOG", "data/gogap.log")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if cfg.LogPath != "data/gogap.log" {
		t.Fatalf("Parse().LogPath = %q, want %q", cfg.LogPath, "data/gogap.log")
	}
}
