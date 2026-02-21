package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyGenerate(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	cmd := NewKeyCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"generate", "-o", keyPath})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("key generate failed: %v", err)
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

	output := buf.String()
	if !strings.Contains(output, "Key generated successfully") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
	if !strings.Contains(output, "Public key: age1") {
		t.Errorf("expected output to contain public key, got: %s", output)
	}

	// Test overwrite with --force
	buf.Reset()
	cmd = NewKeyCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"generate", "-o", keyPath, "--force"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("key generate with --force failed: %v", err)
	}
}

func TestKeyGenerateExistingNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing.key")

	if err := os.WriteFile(keyPath, []byte("existing key"), 0o600); err != nil {
		t.Fatalf("failed to create existing key file: %v", err)
	}

	cmd := NewKeyCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"generate", "-o", keyPath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when key exists without --force, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error message to mention 'already exists', got: %v", err)
	}
}
