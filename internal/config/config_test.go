package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigIncludesServerDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Enabled {
		t.Error("Server.Enabled = true, want false")
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want :8080", cfg.Server.Listen)
	}
}

func TestLoadServerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
server:
  enabled: true
  listen: "127.0.0.1:9090"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Server.Enabled {
		t.Error("Server.Enabled = false, want true")
	}
	if cfg.Server.Listen != "127.0.0.1:9090" {
		t.Errorf("Server.Listen = %q, want 127.0.0.1:9090", cfg.Server.Listen)
	}
}
