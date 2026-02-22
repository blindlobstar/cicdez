package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/blindlobstar/cicdez/internal/vault"
)

func setupTestEnv(t *testing.T) string {
	tmpDir := t.TempDir()

	os.Chdir(tmpDir)

	newIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, ".keys"), 0o700); err != nil {
		t.Fatalf("failed to create key directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, ".keys", "age.key"), []byte(newIdentity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write age key: %v", err)
	}
	t.Setenv("CICDEZ_AGE_KEY_FILE", filepath.Join(tmpDir, ".keys", "age.key"))

	return tmpDir
}

func TestSecretAdd(t *testing.T) {
	setupTestEnv(t)

	cmd := NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "DB_PASSWORD", "secret123"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("secret add failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if secrets["DB_PASSWORD"] != "secret123" {
		t.Errorf("expected DB_PASSWORD to be 'secret123', got '%s'", secrets["DB_PASSWORD"])
	}

	output := buf.String()
	if !strings.Contains(output, "Secret 'DB_PASSWORD' added") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
}

func TestSecretAddUpdate(t *testing.T) {
	setupTestEnv(t)

	cmd := NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "API_KEY", "initial_value"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("secret add failed: %v", err)
	}

	cmd = NewSecretCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "API_KEY", "updated_value"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("secret add (update) failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if secrets["API_KEY"] != "updated_value" {
		t.Errorf("expected API_KEY to be 'updated_value', got '%s'", secrets["API_KEY"])
	}
}

func TestSecretList(t *testing.T) {
	setupTestEnv(t)

	secrets := map[string]string{
		"DB_PASSWORD": "db_secret",
		"API_KEY":     "api_secret",
		"JWT_SECRET":  "jwt_secret",
	}

	for name, value := range secrets {
		cmd := NewSecretCommand()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"add", name, value})
		err := cmd.Execute()
		if err != nil {
			t.Fatalf("secret add failed for %s: %v", name, err)
		}
	}

	cmd := NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("secret list failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Secrets:") {
		t.Errorf("expected output to contain 'Secrets:', got: %s", output)
	}
	for name := range secrets {
		if !strings.Contains(output, name) {
			t.Errorf("expected output to contain '%s', got: %s", name, output)
		}
	}
}

func TestSecretListEmpty(t *testing.T) {
	setupTestEnv(t)

	cmd := NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("secret list failed on empty secrets: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No secrets found") {
		t.Errorf("expected output to contain 'No secrets found', got: %s", output)
	}
}

func TestSecretRemove(t *testing.T) {
	setupTestEnv(t)

	cmd := NewSecretCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "TEMP_SECRET", "temp_value"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("secret add failed: %v", err)
	}

	cmd = NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"remove", "TEMP_SECRET"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("secret remove failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if _, exists := secrets["TEMP_SECRET"]; exists {
		t.Error("expected TEMP_SECRET to be removed, but it still exists")
	}

	output := buf.String()
	if !strings.Contains(output, "Secret 'TEMP_SECRET' removed") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
}

func TestSecretRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	cmd := NewSecretCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "NON_EXISTENT"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when removing non-existent secret, got nil")
	}
}
