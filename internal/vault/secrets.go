package vault

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

var secretsPath = filepath.Join(Dir, "secrets.age")

type Secrets struct {
	Values map[string]string `yaml:"values"`
}

func LoadSecrets(path string) (Secrets, error) {
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

func SaveSecrets(path string, secrets Secrets) error {
	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	if err := EncryptFile(filepath.Join(path, secretsPath), data); err != nil {
		return fmt.Errorf("failed to encrypt secrets: %w", err)
	}

	return nil
}

const (
	SecretOutputEnv  = "env"
	SecretOutputJSON = "json"
	SecretOutputRaw  = "raw"
)

func FormatEnv(secrets map[string]string) []byte {
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []byte
	for _, k := range keys {
		result = append(result, []byte(k+"="+secrets[k]+"\n")...)
	}
	return result
}

func FormatJSON(secrets map[string]string) ([]byte, error) {
	return json.Marshal(secrets)
}

func FormatRaw(value string) []byte {
	return []byte(value)
}

func FormatSecretsForSensitive(allSecrets Secrets, needed []types.SensitiveSecret, format string) ([]byte, error) {
	if len(needed) == 0 {
		return nil, fmt.Errorf("no secrets specified for sensitive config")
	}

	picked := make(map[string]string, len(needed))
	for _, s := range needed {
		value, ok := allSecrets.Values[s.Source]
		if !ok {
			return nil, fmt.Errorf("secret %q not found in cicdez secrets", s.Source)
		}
		outputName := s.Name
		if outputName == "" {
			outputName = s.Source
		}
		picked[outputName] = value
	}

	switch format {
	case SecretOutputEnv, "":
		return FormatEnv(picked), nil
	case SecretOutputJSON:
		return FormatJSON(picked)
	case SecretOutputRaw:
		if len(picked) != 1 {
			return nil, fmt.Errorf("raw format requires exactly one secret, got %d", len(picked))
		}
		for _, v := range picked {
			return FormatRaw(v), nil
		}
	}

	return nil, fmt.Errorf("unknown format: %s", format)
}
