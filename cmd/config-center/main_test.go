package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultServeArgsUsesReadableDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("CONFIG_CENTER_ADDR=:9313\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	chdir(t, dir)

	args, ok := defaultServeArgs()
	if !ok {
		t.Fatal("expected default serve args when .env exists")
	}

	expected := []string{"serve", "--env", envPath, "--migrate"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}

func TestDefaultServeArgsDisabledWithoutDotEnv(t *testing.T) {
	chdir(t, t.TempDir())

	if args, ok := defaultServeArgs(); ok {
		t.Fatalf("expected no default serve args without .env, got %v", args)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}
