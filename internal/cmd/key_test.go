package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyGenerate(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	err := runKeyGenerate(keyGenerateOptions{outputPath: keyPath})
	if err != nil {
		t.Fatalf("runKeyGenerate failed: %v", err)
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read generated key: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# created:") {
		t.Error("expected key file to contain '# created:' comment")
	}
	if !strings.Contains(content, "# public key: age1") {
		t.Error("expected key file to contain public key comment")
	}
	if !strings.Contains(content, "AGE-SECRET-KEY-") {
		t.Error("expected key file to contain secret key")
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("failed to stat key file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected file permissions 0600, got %o", info.Mode().Perm())
	}

	// Test overwrite with --force
	err = runKeyGenerate(keyGenerateOptions{outputPath: keyPath, force: true})
	if err != nil {
		t.Fatalf("runKeyGenerate with --force failed: %v", err)
	}
}

func TestKeyGenerateExistingNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing.key")

	if err := os.WriteFile(keyPath, []byte("existing key"), 0o600); err != nil {
		t.Fatalf("failed to create existing key file: %v", err)
	}

	err := runKeyGenerate(keyGenerateOptions{outputPath: keyPath})
	if err == nil {
		t.Error("expected error when key exists without --force, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error message to mention 'already exists', got: %v", err)
	}
}
