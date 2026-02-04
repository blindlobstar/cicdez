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

func loadSecrets(path string) (Secrets, error) {
	var secrets Secrets

	data, err := DecryptFile(filepath.Join(path, secretsPath))
	if err != nil {
		return secrets, fmt.Errorf("failed to decrypt secrets: %w", err)
	}

	if err := yaml.Unmarshal(data, &secrets); err != nil {
		return secrets, fmt.Errorf("failed to parse secrets: %w", err)
	}

	return secrets, nil
}

func saveSecrets(path string, secrets Secrets) error {
	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	if err := EncryptFile(filepath.Join(path, secretsPath), data); err != nil {
		return fmt.Errorf("failed to encrypt secrets: %w", err)
	}

	return nil
}
