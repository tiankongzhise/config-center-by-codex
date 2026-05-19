package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsConfigCenterBaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CONFIG_CENTER_BASE_URL=http://127.0.0.1:18080\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.BaseURL != "http://127.0.0.1:18080" {
		t.Fatalf("unexpected base url %q", cfg.BaseURL)
	}
}
