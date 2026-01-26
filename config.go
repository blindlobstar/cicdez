package main

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var configPath string = filepath.Join(cicdezDir, "config.age")

type Config struct {
	Servers    map[string]Server   `yaml:"servers"`
	Registries map[string]Registry `yaml:"registries"`
}

type Server struct {
	Host string `yaml:"host"`
	User string `yaml:"user"`
	Key  string `yaml:"key"`
}

type Registry struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func loadConfig(e *encrypter, path string) (Config, error) {
	var config Config

	if err := e.LoadIdentity(); err != nil {
		return config, err
	}

	data, err := e.DecryptFile(filepath.Join(path, configPath))
	if err != nil {
		return config, fmt.Errorf("failed to decrypt config: %w", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config: %w", err)
	}

	return config, nil
}

func saveConfig(e *encrypter, path string, config Config) error {
	if err := e.LoadRecipients(); err != nil {
		return err
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := e.EncryptFile(data, filepath.Join(path, configPath)); err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	return nil
}
