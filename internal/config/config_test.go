package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateDotEnvPreservesExistingValuesAndAddsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := "PG_HOST=127.0.0.1\n# comment\nCONFIG_CENTER_DB_USER=old\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := UpdateDotEnv(path, map[string]string{
		"CONFIG_CENTER_DB_USER":     "config_center",
		"CONFIG_CENTER_DB_PASSWORD": "secret value",
	}); err != nil {
		t.Fatalf("update env: %v", err)
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	content := string(contentBytes)
	if !strings.Contains(content, "PG_HOST=127.0.0.1") {
		t.Fatal("expected existing value to be preserved")
	}
	if !strings.Contains(content, "CONFIG_CENTER_DB_USER=config_center") {
		t.Fatal("expected existing key to be updated")
	}
	if !strings.Contains(content, `CONFIG_CENTER_DB_PASSWORD="secret value"`) {
		t.Fatal("expected missing key to be appended and quoted")
	}
}

func TestClearDotEnvValuesKeepsKeysButRemovesSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := "PG_ADMIN=postgres\nPG_ADMIN_SECRET=secret\nCONFIG_CENTER_DB_USER=config_center\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	if err := ClearDotEnvValues(path, "PG_ADMIN", "PG_ADMIN_SECRET"); err != nil {
		t.Fatalf("clear env: %v", err)
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	content := string(contentBytes)
	if strings.Contains(content, "postgres") || strings.Contains(content, "secret") {
		t.Fatalf("expected bootstrap credentials to be cleared, got %q", content)
	}
	if !strings.Contains(content, "PG_ADMIN=\n") || !strings.Contains(content, "PG_ADMIN_SECRET=\n") {
		t.Fatalf("expected keys to remain with empty values, got %q", content)
	}
	if !strings.Contains(content, "CONFIG_CENTER_DB_USER=config_center") {
		t.Fatal("expected unrelated values to be preserved")
	}
}

func TestLoadAcceptsSpacesAroundEnvEqualsAndEscapesDatabaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := strings.Join([]string{
		"PG_HOST = pgmq.baichengedu.com",
		"PG_PORT = 54320",
		"CONFIG_CENTER_DB_NAME=config_center",
		"CONFIG_CENTER_DB_USER=config_center",
		`CONFIG_CENTER_DB_PASSWORD="pass word+percent%slash/at@colon:"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load env: %v", err)
	}

	if cfg.PostgresHost != "pgmq.baichengedu.com" || cfg.PostgresPort != "54320" {
		t.Fatalf("expected configured pg target, got %s:%s", cfg.PostgresHost, cfg.PostgresPort)
	}
	if target := cfg.DatabaseTarget(); target != "host=pgmq.baichengedu.com port=54320 database=config_center user=config_center" {
		t.Fatalf("unexpected database target: %s", target)
	}
	if !strings.Contains(cfg.DatabaseURL, "pass%20word+percent%25slash%2Fat%40colon%3A") {
		t.Fatalf("expected database URL password to be escaped, got %q", cfg.DatabaseURL)
	}
}
