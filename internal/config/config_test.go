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
