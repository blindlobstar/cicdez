package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"gopkg.in/yaml.v3"
)

func setupTestKey(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	newIdentity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}

	keyPath := filepath.Join(tmpDir, "age.key")
	if err := os.WriteFile(keyPath, []byte(newIdentity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write age key: %v", err)
	}
	t.Setenv(EnvAgeKeyPath, keyPath)
	identity = nil

	return tmpDir
}

func TestSecretsRoundTrip(t *testing.T) {
	dir := setupTestKey(t)

	want := Secrets{"DB_PASSWORD": "secret123", "API_KEY": "mykey"}
	if err := SaveSecrets(dir, want); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}

	got, err := LoadSecrets(dir)
	if err != nil {
		t.Fatalf("LoadSecrets failed: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d secrets, got %d", len(want), len(got))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("expected %s=%q, got %q", k, v, got[k])
		}
	}
}

func TestSaveSecretsPreservesCiphertext(t *testing.T) {
	dir := setupTestKey(t)

	if err := SaveSecrets(dir, Secrets{"KEEP": "same", "CHANGE": "old"}); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}

	readFile := func() map[string]string {
		data, err := os.ReadFile(filepath.Join(dir, secretsPath))
		if err != nil {
			t.Fatalf("failed to read secrets file: %v", err)
		}
		var raw map[string]string
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to parse secrets file: %v", err)
		}
		return raw
	}

	before := readFile()
	if before["KEEP"] == "same" || before["CHANGE"] == "old" {
		t.Fatal("secrets file contains plaintext values")
	}

	if err := SaveSecrets(dir, Secrets{"KEEP": "same", "CHANGE": "new"}); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}

	after := readFile()
	if after["KEEP"] != before["KEEP"] {
		t.Error("expected unchanged secret to keep its ciphertext")
	}
	if after["CHANGE"] == before["CHANGE"] {
		t.Error("expected changed secret to get new ciphertext")
	}
}

func TestLoadSecretsMissing(t *testing.T) {
	dir := setupTestKey(t)

	secrets, err := LoadSecrets(dir)
	if err != nil {
		t.Fatalf("LoadSecrets failed on missing file: %v", err)
	}
	if len(secrets) != 0 {
		t.Errorf("expected no secrets, got %d", len(secrets))
	}
}

func TestParseSecrets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Secrets
		wantErr error
	}{
		{
			name:  "flat secrets",
			input: "DB_PASSWORD: secret123\nAPI_KEY: mykey\n",
			want: Secrets{
				"DB_PASSWORD": "secret123",
				"API_KEY":     "mykey",
			},
		},
		{
			name:  "empty",
			input: "",
			want:  Secrets{},
		},
		{
			name:    "nested map",
			input:   "database:\n  password: secret123\n",
			wantErr: ErrNestedSecret,
		},
		{
			name:    "nested array",
			input:   "keys:\n  - key1\n  - key2\n",
			wantErr: ErrNestedSecret,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSecrets([]byte(tt.input))

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d secrets, got %d", len(tt.want), len(got))
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("expected %s=%q, got %q", k, v, got[k])
				}
			}
		})
	}
}
