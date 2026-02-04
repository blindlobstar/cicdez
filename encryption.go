package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

const envAgeKeyPath = "CICDEZ_AGE_KEY_FILE"

var recipientsPath string = filepath.Join(cicdezDir, "recipients.txt")

var identity *age.X25519Identity

func EncryptFile(path string, data []byte) error {
	if err := loadIdentity(); err != nil {
		return fmt.Errorf("failed to load recipients: %w", err)
	}

	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, identity.Recipient())
	if err != nil {
		return fmt.Errorf("failed to create encrypt: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to encrypt data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize encryption: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	if _, err := file.Write(encrypted.Bytes()); err != nil {
		return fmt.Errorf("failed to write encrypted file: %w", err)
	}

	return nil
}

func DecryptFile(path string) ([]byte, error) {
	if err := loadIdentity(); err != nil {
		return nil, fmt.Errorf("failed to load identity: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open encrypted file: %w", err)
	}
	defer file.Close()

	r, err := age.Decrypt(file, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	decrypted, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data: %w", err)
	}

	return decrypted, nil
}

func loadIdentity() error {
	if identity != nil {
		return nil
	}

	kp, err := getKeyPath()
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

func getKeyPath() (string, error) {
	if envPath := os.Getenv(envAgeKeyPath); envPath != "" {
		return envPath, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "cicdez", "age.key"), nil
}
