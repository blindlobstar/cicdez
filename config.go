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

func loadConfig(path string) (Config, error) {
	var config Config

	data, err := DecryptFile(filepath.Join(path, configPath))
	if err != nil {
		return config, fmt.Errorf("failed to decrypt config: %w", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config: %w", err)
	}

	return config, nil
}

func saveConfig(path string, config Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := EncryptFile(filepath.Join(path, configPath), data); err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	return nil
}
