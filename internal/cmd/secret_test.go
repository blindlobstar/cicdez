package cmd

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"filippo.io/age"
	"github.com/vrotherford/cicdez/internal/vault"
)

func setupTestEnv(t *testing.T) string {
	tmpDir := t.TempDir()

	os.Chdir(tmpDir)

	newIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}
	vault.SetIdentity(newIdentity)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".keys"), 0o700); err != nil {
		t.Fatalf("failed to create key directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, ".keys", "age.key"), []byte(newIdentity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write age key: %v", err)
	}
	os.Setenv("CICDEZ_AGE_KEY_PATH", filepath.Join(tmpDir, ".keys", "age.key"))

	os.Stdout = os.NewFile(uintptr(syscall.Stdin), os.DevNull)
	t.Cleanup(func() {
		os.Stdout.Close()
	})

	return tmpDir
}

func TestSecretAdd(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(&secretAddOptions{name: "DB_PASSWORD", value: "secret123"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if secrets.Values["DB_PASSWORD"] != "secret123" {
		t.Errorf("expected DB_PASSWORD to be 'secret123', got '%s'", secrets.Values["DB_PASSWORD"])
	}
}

func TestSecretAddUpdate(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(&secretAddOptions{name: "API_KEY", value: "initial_value"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	err = runSecretAdd(&secretAddOptions{name: "API_KEY", value: "updated_value"})
	if err != nil {
		t.Fatalf("runSecretAdd (update) failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if secrets.Values["API_KEY"] != "updated_value" {
		t.Errorf("expected API_KEY to be 'updated_value', got '%s'", secrets.Values["API_KEY"])
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
		err := runSecretAdd(&secretAddOptions{name: name, value: value})
		if err != nil {
			t.Fatalf("runSecretAdd failed for %s: %v", name, err)
		}
	}

	err := runSecretList()
	if err != nil {
		t.Fatalf("runSecretList failed: %v", err)
	}
}

func TestSecretListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runSecretList()
	if err != nil {
		t.Fatalf("runSecretList failed on empty secrets: %v", err)
	}
}

func TestSecretRemove(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(&secretAddOptions{name: "TEMP_SECRET", value: "temp_value"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	err = runSecretRemove(&secretRemoveOptions{name: "TEMP_SECRET"})
	if err != nil {
		t.Fatalf("runSecretRemove failed: %v", err)
	}

	secrets, err := vault.LoadSecrets(".")
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if _, exists := secrets.Values["TEMP_SECRET"]; exists {
		t.Error("expected TEMP_SECRET to be removed, but it still exists")
	}
}

func TestSecretRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runSecretRemove(&secretRemoveOptions{name: "NON_EXISTENT"})
	if err == nil {
		t.Error("expected error when removing non-existent secret, got nil")
	}
}
