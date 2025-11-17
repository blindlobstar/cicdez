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

func loadIdentity() (age.Identity, error) {
	keyPath, err := getKeyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get key path: %w", err)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read age key from %s: %w", keyPath, err)
	}

	identities, err := age.ParseIdentities(strings.NewReader(string(keyData)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse age key: %w", err)
	}

	if len(identities) == 0 {
		return nil, fmt.Errorf("no valid age identity found in %s", keyPath)
	}

	return identities[0], nil
}

func loadRecipients(path string) ([]age.Recipient, error) {
	data, err := os.ReadFile(filepath.Join(path, recipientsPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read recipients file: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	recipients := make([]age.Recipient, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		recipient, err := age.ParseX25519Recipient(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse recipient %s: %w", line, err)
		}

		recipients = append(recipients, recipient)
	}

	return recipients, nil
}

func addRecipient(path string, publicKey string) error {
	var existingKeys []string
	fullPath := filepath.Join(path, recipientsPath)
	data, err := os.ReadFile(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read recipients file: %w", err)
	}

	if len(data) > 0 {
		lines := strings.SplitSeq(strings.TrimSpace(string(data)), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if line == publicKey {
				return fmt.Errorf("recipient already exists")
			}
			existingKeys = append(existingKeys, line)
		}
	}

	existingKeys = append(existingKeys, publicKey)
	content := strings.Join(existingKeys, "\n") + "\n"

	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write recipients file: %w", err)
	}

	return nil
}

func encryptFile(recipients []age.Recipient, path string, data []byte) error {
	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, recipients...)
	if err != nil {
		return fmt.Errorf("failed to create encryptor: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to encrypt data: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize encryption: %w", err)
	}

	if err := os.WriteFile(path, encrypted.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write encrypted file: %w", err)
	}

	return nil
}

func decryptFile(identity age.Identity, path string) ([]byte, error) {
	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted file: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(encryptedData), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	decrypted, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data: %w", err)
	}

	return decrypted, nil
}
