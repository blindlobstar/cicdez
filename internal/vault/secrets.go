package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"text/template"

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
	SecretOutputEnv      = "env"
	SecretOutputJSON     = "json"
	SecretOutputRaw      = "raw"
	SecretOutputTemplate = "template"
)

func pickSecrets(allSecrets Secrets, needed []types.SensitiveSecret) (map[string]string, error) {
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

	return picked, nil
}

func FormatEnv(allSecrets Secrets, needed []types.SensitiveSecret) ([]byte, error) {
	picked, err := pickSecrets(allSecrets, needed)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(picked))
	for k := range picked {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []byte
	for _, k := range keys {
		result = append(result, []byte(k+"="+picked[k]+"\n")...)
	}
	return result, nil
}

func FormatJSON(allSecrets Secrets, needed []types.SensitiveSecret) ([]byte, error) {
	picked, err := pickSecrets(allSecrets, needed)
	if err != nil {
		return nil, err
	}
	return json.Marshal(picked)
}

func FormatRaw(allSecrets Secrets, needed []types.SensitiveSecret) ([]byte, error) {
	picked, err := pickSecrets(allSecrets, needed)
	if err != nil {
		return nil, err
	}
	if len(picked) != 1 {
		return nil, fmt.Errorf("raw format requires exactly one secret, got %d", len(picked))
	}
	for _, v := range picked {
		return []byte(v), nil
	}
	return nil, nil
}

func FormatTemplate(allSecrets Secrets, needed []types.SensitiveSecret, templateContent string) ([]byte, error) {
	picked, err := pickSecrets(allSecrets, needed)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("sensitive").Parse(templateContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, picked); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}
