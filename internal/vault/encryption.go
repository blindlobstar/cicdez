package vault

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

const EnvAgeKeyPath = "CICDEZ_AGE_KEY_FILE"

const valuePrefix = "age:"

var identity *age.X25519Identity

func EncryptValue(data []byte) (string, error) {
	if err := loadIdentity(); err != nil {
		return "", fmt.Errorf("failed to load identity: %w", err)
	}

	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, identity.Recipient())
	if err != nil {
		return "", fmt.Errorf("failed to create encrypt: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("failed to encrypt data: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize encryption: %w", err)
	}

	return valuePrefix + base64.StdEncoding.EncodeToString(encrypted.Bytes()), nil
}

func DecryptValue(value string) ([]byte, error) {
	if err := loadIdentity(); err != nil {
		return nil, fmt.Errorf("failed to load identity: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, valuePrefix))
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted value: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(raw), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	decrypted, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data: %w", err)
	}

	return decrypted, nil
}

func writeVaultFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}
	return nil
}

func loadIdentity() error {
	if identity != nil {
		return nil
	}

	kp, err := GetKeyPath()
	if err != nil {
		return fmt.Errorf("failed to get key path: %w", err)
	}

	kd, err := os.ReadFile(kp)
	if err != nil {
		return fmt.Errorf("failed to read age key from %s: %w", kp, err)
	}

	identities, err := age.ParseIdentities(strings.NewReader(string(kd)))
	if err != nil {
		return fmt.Errorf("failed to parse age key: %w", err)
	}

	if len(identities) == 0 {
		return fmt.Errorf("no valid age identity found in %s", kp)
	}

	identity = identities[0].(*age.X25519Identity)

	return nil
}

func GetKeyPath() (string, error) {
	if envPath := os.Getenv(EnvAgeKeyPath); envPath != "" {
		return envPath, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "cicdez", "age.key"), nil
}
