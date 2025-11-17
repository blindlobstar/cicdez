package main

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestEnv(t *testing.T) string {
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	keyDir := t.TempDir()
	os.Setenv(envAgeKeyPath, filepath.Join(keyDir, "age.key"))

	err := runInit(nil, nil)
	if err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	return tmpDir
}

func TestSecretAdd(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(nil, []string{"DB_PASSWORD", "secret123"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		t.Fatalf("loadIdentity failed: %v", err)
	}

	secrets, err := loadSecrets(".", identity)
	if err != nil {
		t.Fatalf("loadSecrets failed: %v", err)
	}

	if secrets.Values["DB_PASSWORD"] != "secret123" {
		t.Errorf("expected DB_PASSWORD to be 'secret123', got '%s'", secrets.Values["DB_PASSWORD"])
	}
}

func TestSecretAddUpdate(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(nil, []string{"API_KEY", "initial_value"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	err = runSecretAdd(nil, []string{"API_KEY", "updated_value"})
	if err != nil {
		t.Fatalf("runSecretAdd (update) failed: %v", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		t.Fatalf("loadIdentity failed: %v", err)
	}

	secrets, err := loadSecrets(".", identity)
	if err != nil {
		t.Fatalf("loadSecrets failed: %v", err)
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
		err := runSecretAdd(nil, []string{name, value})
		if err != nil {
			t.Fatalf("runSecretAdd failed for %s: %v", name, err)
		}
	}

	err := runSecretList(nil, nil)
	if err != nil {
		t.Fatalf("runSecretList failed: %v", err)
	}
}

func TestSecretListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runSecretList(nil, nil)
	if err != nil {
		t.Fatalf("runSecretList failed on empty secrets: %v", err)
	}
}

func TestSecretRemove(t *testing.T) {
	setupTestEnv(t)

	err := runSecretAdd(nil, []string{"TEMP_SECRET", "temp_value"})
	if err != nil {
		t.Fatalf("runSecretAdd failed: %v", err)
	}

	err = runSecretRemove(nil, []string{"TEMP_SECRET"})
	if err != nil {
		t.Fatalf("runSecretRemove failed: %v", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		t.Fatalf("loadIdentity failed: %v", err)
	}

	secrets, err := loadSecrets(".", identity)
	if err != nil {
		t.Fatalf("loadSecrets failed: %v", err)
	}

	if _, exists := secrets.Values["TEMP_SECRET"]; exists {
		t.Error("expected TEMP_SECRET to be removed, but it still exists")
	}
}

func TestSecretRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runSecretRemove(nil, []string{"NON_EXISTENT"})
	if err == nil {
		t.Error("expected error when removing non-existent secret, got nil")
	}
}
