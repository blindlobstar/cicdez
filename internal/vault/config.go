package vault

import (
	"fmt"
	"path/filepath"

	"github.com/moby/moby/api/types/registry"
	"gopkg.in/yaml.v3"
)

const Dir = ".cicdez"

var configPath = filepath.Join(Dir, "config.age")

type Config struct {
	Servers       map[string]Server              `yaml:"servers"`
	Registries    map[string]registry.AuthConfig `yaml:"registries"`
	DefaultServer string                         `yaml:"default_server,omitempty"`
}

func (c *Config) AddServer(name string, server Server) {
	if c.Servers == nil {
		c.Servers = make(map[string]Server)
	}
	c.Servers[name] = server
	if c.DefaultServer == "" {
		c.DefaultServer = name
	}
}

func (c *Config) RemoveServer(name string) string {
	delete(c.Servers, name)
	if c.DefaultServer == name {
		c.DefaultServer = ""
		for n := range c.Servers {
			c.DefaultServer = n
			return n
		}
	}
	return ""
}

func (c *Config) SetDefault(name string) error {
	if _, ok := c.Servers[name]; !ok {
		return fmt.Errorf("server %q not found", name)
	}
	c.DefaultServer = name
	return nil
}

func (c *Config) GetServer(name string) (Server, error) {
	if name == "" {
		name = c.DefaultServer
	}
	if name == "" {
		return Server{}, fmt.Errorf("no server specified and no default set")
	}
	s, ok := c.Servers[name]
	if !ok {
		return Server{}, fmt.Errorf("server %q not found", name)
	}
	return s, nil
}

type Server struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port,omitempty"`
	User string `yaml:"user"`
	Key  string `yaml:"key"`
}

func LoadConfig(path string) (Config, error) {
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

func SaveConfig(path string, config Config) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := EncryptFile(filepath.Join(path, configPath), data); err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	return nil
}
