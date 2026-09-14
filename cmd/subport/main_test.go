package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvPreservesExplicitEmptyValue(t *testing.T) {
	const key = "SUBPORT_TEST_EMPTY_OVERRIDE"
	t.Setenv(key, "")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(key+"=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadDotEnv(path)
	if got := os.Getenv(key); got != "" {
		t.Fatalf("explicit empty environment value overwritten with %q", got)
	}
}
