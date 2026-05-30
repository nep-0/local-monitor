package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database DatabaseConfig `yaml:"database"`
	Monitor  MonitorConfig  `yaml:"monitor"`
	Server   ServerConfig   `yaml:"server"`
	Devices  []Device       `yaml:"devices"`
	Logging  LoggingConfig  `yaml:"logging"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type MonitorConfig struct {
	Interval     time.Duration `yaml:"interval"`
	Timeout      time.Duration `yaml:"timeout"`
	Interface    string        `yaml:"interface"`
	Workers      int           `yaml:"workers"`
	RetryCount   int           `yaml:"retry_count"`
	RetryDelay   time.Duration `yaml:"retry_delay"`
	StartupProbe bool          `yaml:"startup_probe"`
	StartupDelay time.Duration `yaml:"startup_delay"`
}

type ServerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

type Device struct {
	Name  string `yaml:"name"`
	IP    string `yaml:"ip"`
	MAC   string `yaml:"mac,omitempty"`
	Group string `yaml:"group,omitempty"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Database: DatabaseConfig{
			Path: "local-monitor.db",
		},
		Monitor: MonitorConfig{
			Interval:     60 * time.Second,
			Timeout:      2 * time.Second,
			Workers:      10,
			RetryCount:   3,
			RetryDelay:   1 * time.Second,
			StartupProbe: true,
			StartupDelay: 5 * time.Second,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Server: ServerConfig{
			Enabled: false,
			Listen:  ":8080",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		Database: DatabaseConfig{
			Path: "local-monitor.db",
		},
		Monitor: MonitorConfig{
			Interval:     60 * time.Second,
			Timeout:      2 * time.Second,
			Workers:      10,
			RetryCount:   3,
			RetryDelay:   1 * time.Second,
			StartupProbe: true,
			StartupDelay: 5 * time.Second,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Server: ServerConfig{
			Enabled: false,
			Listen:  ":8080",
		},
	}
}
