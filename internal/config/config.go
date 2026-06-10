package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	SourceEastMoney = "eastmoney"
)

type SourceConfig struct {
	Live LiveSourceConfig `json:"live"`
}

type LiveSourceConfig struct {
	Quote []string `json:"quote"`
	NAV   []string `json:"nav"`
}

func (c LiveSourceConfig) Names() []string {
	if len(c.Quote) == 0 && len(c.NAV) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(c.Quote)+len(c.NAV))
	appendName := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, name := range c.Quote {
		appendName(name)
	}
	for _, name := range c.NAV {
		appendName(name)
	}
	return names
}

type Config struct {
	Addr         string        `json:"addr"`
	DBPath       string        `json:"dbPath"`
	LogPath      string        `json:"logPath"`
	PollInterval time.Duration `json:"pollInterval"`
	Dev          bool          `json:"dev"`
	Sources      SourceConfig  `json:"sources"`
}

func Default() Config {
	return Config{
		Addr:         "127.0.0.1:8080",
		DBPath:       "data/gogap.db",
		PollInterval: 120 * time.Second,
		Dev:          false,
		Sources: SourceConfig{
			Live: LiveSourceConfig{Quote: []string{SourceEastMoney}, NAV: []string{SourceEastMoney}},
		},
	}
}

func Parse(args []string) (Config, error) {
	cfg := Default()
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	fs := flag.NewFlagSet("gogap", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address")
	fs.StringVar(&cfg.DBPath, "db", cfg.DBPath, "database path")
	fs.StringVar(&cfg.LogPath, "log", cfg.LogPath, "log file path")
	fs.DurationVar(&cfg.PollInterval, "poll-interval", cfg.PollInterval, "polling interval")
	fs.BoolVar(&cfg.Dev, "dev", cfg.Dev, "enable development mode")
	var sourceNames string
	fs.StringVar(&sourceNames, "sources", strings.Join(cfg.Sources.Live.Names(), ","), "comma-separated source names for quote/nav")
	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if sourceNames != "" {
		names := splitNames(sourceNames)
		for _, name := range names {
			if name != SourceEastMoney {
				return Config{}, fmt.Errorf("parse flags: unknown source %q", name)
			}
		}
		cfg.Sources.Live = LiveSourceConfig{Quote: names, NAV: names}
	}
	if len(cfg.Sources.Live.Quote) == 0 {
		cfg.Sources.Live.Quote = []string{SourceEastMoney}
	}
	if len(cfg.Sources.Live.NAV) == 0 {
		cfg.Sources.Live.NAV = []string{SourceEastMoney}
	}
	return cfg, nil
}

func loadDotEnv(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load dotenv %s: %w", path, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) || (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set dotenv %s: %w", key, err)
			}
		}
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if value, ok := os.LookupEnv("GOGAP_ADDR"); ok && value != "" {
		cfg.Addr = value
	}
	if value, ok := os.LookupEnv("GOGAP_DB"); ok && value != "" {
		cfg.DBPath = value
	}
	if value, ok := os.LookupEnv("GOGAP_LOG"); ok && value != "" {
		cfg.LogPath = value
	}
	if value, ok := os.LookupEnv("GOGAP_POLL_INTERVAL"); ok && value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("parse GOGAP_POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}
	if value, ok := os.LookupEnv("GOGAP_DEV"); ok && value != "" {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse GOGAP_DEV: %w", err)
		}
		cfg.Dev = b
	}
	if value, ok := os.LookupEnv("GOGAP_SOURCES"); ok && value != "" {
		names := splitNames(value)
		if len(names) != 0 {
			cfg.Sources.Live = LiveSourceConfig{Quote: names, NAV: names}
		}
	}
	return nil
}

func splitNames(value string) []string {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
