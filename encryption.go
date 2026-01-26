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

type encrypter struct {
	Identity       age.Identity
	recipients     []age.Recipient
	keyPath        string
	recipientsPath string
}

func NewEncrypter(cwd string) (*encrypter, error) {
	encrypter := &encrypter{}
	if path, err := getKeyPath(); err != nil {
		return encrypter, err
	} else {
		encrypter.keyPath = path
	}

	encrypter.recipientsPath = filepath.Join(cwd, recipientsPath)

	return encrypter, nil
}

func (e *encrypter) LoadIdentity() error {
	if e.Identity != nil {
		return nil
	}
	keyData, err := os.ReadFile(e.keyPath)
	if err != nil {
		return fmt.Errorf("failed to read age key from %s: %w", e.keyPath, err)
	}

	identities, err := age.ParseIdentities(strings.NewReader(string(keyData)))
	if err != nil {
		return fmt.Errorf("failed to parse age key: %w", err)
	}

	if len(identities) == 0 {
		return fmt.Errorf("no valid age identity found in %s", e.keyPath)
	}

	e.Identity = identities[0]

	return nil
}

func (e *encrypter) LoadRecipients() error {
	if e.recipients != nil {
		return nil
	}
	data, err := os.ReadFile(e.recipientsPath)
	if err != nil {
		return fmt.Errorf("failed to read recipients file: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	e.recipients = make([]age.Recipient, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		recipient, err := age.ParseX25519Recipient(line)
		if err != nil {
			return fmt.Errorf("failed to parse recipient %s: %w", line, err)
		}

		e.recipients = append(e.recipients, recipient)
	}
	return nil
}

func (e *encrypter) AddRecipient(publicKey string) error {
	for _, r := range e.recipients {
		if x25519, ok := r.(*age.X25519Recipient); ok && x25519.String() == publicKey {
			return fmt.Errorf("recipient already exists")
		}
	}

	recipient, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return err
	}
	e.recipients = append(e.recipients, recipient)

	var content string
	for _, r := range e.recipients {
		content += fmt.Sprintf("%s\n", r)
	}

	if err := os.WriteFile(e.recipientsPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write recipients file: %w", err)
	}

	return nil
}

func (e *encrypter) EncryptFile(data []byte, path string) error {
	var encrypted bytes.Buffer
	w, err := age.Encrypt(&encrypted, e.recipients...)
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

func (e *encrypter) DecryptFile(path string) ([]byte, error) {
	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted file: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(encryptedData), e.Identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	decrypted, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read decrypted data: %w", err)
	}

	return decrypted, nil
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
