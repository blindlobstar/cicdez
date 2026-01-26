package main

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var secretsPath string = filepath.Join(cicdezDir, "secrets.age")

type Secrets struct {
	Values map[string]string `yaml:"values"`
}

func loadSecrets(e *encrypter, path string) (Secrets, error) {
	var secrets Secrets

	if err := e.LoadIdentity(); err != nil {
		return secrets, err
	}

	data, err := e.DecryptFile(filepath.Join(path, secretsPath))
	if err != nil {
		return secrets, fmt.Errorf("failed to decrypt secrets: %w", err)
	}

	if err := yaml.Unmarshal(data, &secrets); err != nil {
		return secrets, fmt.Errorf("failed to parse secrets: %w", err)
	}

	return secrets, nil
}

func saveSecrets(e *encrypter, path string, secrets Secrets) error {
	if err := e.LoadRecipients(); err != nil {
		return err
	}

	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	if err := e.EncryptFile(data, filepath.Join(path, secretsPath)); err != nil {
		return fmt.Errorf("failed to encrypt secrets: %w", err)
	}

	return nil
}
