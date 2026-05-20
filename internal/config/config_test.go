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
