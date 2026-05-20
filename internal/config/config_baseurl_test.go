package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidatePublicServiceURLRejectsLocalhost(t *testing.T) {
	cfg := Config{BaseURL: "http://127.0.0.1:18080"}
	if err := cfg.ValidatePublicServiceURL(); err == nil {
		t.Fatal("expected local http service url to be rejected")
	}
}

func TestValidatePublicServiceURLAllowsHTTPS(t *testing.T) {
	cfg := Config{BaseURL: "https://config-center.example.com"}
	if err := cfg.ValidatePublicServiceURL(); err != nil {
		t.Fatalf("expected public https url to pass: %v", err)
	}
}

func TestLoadAuthLimitOperatorDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CONFIG_CENTER_BASE_URL=https://config-service.baichengedu.com\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AuthLimitOperatorUsername != "config_center_operator" {
		t.Fatalf("unexpected operator username %q", cfg.AuthLimitOperatorUsername)
	}
	if cfg.AuthLimitServiceName != "配置中心" {
		t.Fatalf("unexpected service name %q", cfg.AuthLimitServiceName)
	}
}

func TestLoadStoresResolvedEnvPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CONFIG_CENTER_BASE_URL=https://config-service.baichengedu.com\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.EnvPath != path {
		t.Fatalf("expected env path %q, got %q", path, cfg.EnvPath)
	}
}

func TestResolveEnvPathMissingGivesActionableError(t *testing.T) {
	_, err := ResolveEnvPath(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil {
		t.Fatal("expected missing env file error")
	}
	if !strings.Contains(err.Error(), "--env /www/wwwroot/config-service.baichengedu.com/.env") {
		t.Fatalf("expected actionable error, got %v", err)
	}
}
