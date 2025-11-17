package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunInit(t *testing.T) {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	keyDir := t.TempDir()
	os.Setenv(envAgeKeyPath, filepath.Join(keyDir, "age.key"))

	err := runInit(nil, nil)
	if err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	if _, err := os.Stat(cicdezDir); os.IsNotExist(err) {
		t.Error("expected .cicdez directory to be created")
	}

	keyPath := os.Getenv(envAgeKeyPath)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected key permissions 0600, got %o", info.Mode().Perm())
	}

	if _, err := os.Stat(filepath.Join(cicdezDir, "config.age")); err != nil {
		t.Error("config.age not created")
	}
	if _, err := os.Stat(filepath.Join(cicdezDir, "secrets.age")); err != nil {
		t.Error("secrets.age not created")
	}
	if _, err := os.Stat(filepath.Join(cicdezDir, "recipients.txt")); err != nil {
		t.Error("recipients.txt not created")
	}
}
