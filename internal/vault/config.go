package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

const Dir = ".cicdez"

var configPath = filepath.Join(Dir, "config.yaml")

type Config struct {
	Servers map[string]Server `yaml:"servers"`
}

type Server struct {
	Port int        `yaml:"port,omitempty"`
	User string     `yaml:"user"`
	Key  PrivateKey `yaml:"key"`
}

type PrivateKey []byte

func (k PrivateKey) MarshalYAML() (any, error) {
	return string(k), nil
}

func (k *PrivateKey) UnmarshalYAML(node *yaml.Node) error {
	*k = []byte(node.Value)
	return nil
}

// whole record is encrypted per entry, one line per server, so hosts stay
// private while entries remain mergeable line by line
type serverRecord struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	User string `json:"user"`
	Key  []byte `json:"key,omitempty"`
}

type configFile struct {
	Servers []string `yaml:"servers"`
}

type serverEntry struct {
	cipher string
	plain  []byte
	record serverRecord
}

func LoadConfig(path string) (Config, error) {
	var config Config

	data, err := os.ReadFile(filepath.Join(path, configPath))
	if os.IsNotExist(err) {
		return config, nil
	}
	if err != nil {
		return config, fmt.Errorf("failed to read config: %w", err)
	}

	entries, err := parseServerEntries(data)
	if err != nil {
		return config, err
	}

	// duplicate hosts can appear after a merge; last one wins
	config.Servers = make(map[string]Server, len(entries))
	for _, e := range entries {
		config.Servers[e.record.Host] = Server{Port: e.record.Port, User: e.record.User, Key: e.record.Key}
	}

	return config, nil
}

func SaveConfig(path string, config Config) error {
	var existing []serverEntry
	if data, err := os.ReadFile(filepath.Join(path, configPath)); err == nil {
		if existing, err = parseServerEntries(data); err != nil {
			return err
		}
	}

	// keep existing entries in file order with unchanged ciphertext intact,
	// so a save only touches the lines that actually changed
	cf := configFile{Servers: make([]string, 0, len(config.Servers))}
	saved := make(map[string]bool, len(config.Servers))
	for _, e := range existing {
		server, ok := config.Servers[e.record.Host]
		if !ok || saved[e.record.Host] {
			continue
		}
		saved[e.record.Host] = true

		plain, err := marshalServerRecord(e.record.Host, server)
		if err != nil {
			return err
		}
		if bytes.Equal(e.plain, plain) {
			cf.Servers = append(cf.Servers, e.cipher)
			continue
		}

		cipher, err := EncryptValue(plain)
		if err != nil {
			return fmt.Errorf("failed to encrypt server %q: %w", e.record.Host, err)
		}
		cf.Servers = append(cf.Servers, cipher)
	}

	added := make([]string, 0, len(config.Servers))
	for host := range config.Servers {
		if !saved[host] {
			added = append(added, host)
		}
	}
	sort.Strings(added)

	for _, host := range added {
		plain, err := marshalServerRecord(host, config.Servers[host])
		if err != nil {
			return err
		}
		cipher, err := EncryptValue(plain)
		if err != nil {
			return fmt.Errorf("failed to encrypt server %q: %w", host, err)
		}
		cf.Servers = append(cf.Servers, cipher)
	}

	data, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return writeVaultFile(filepath.Join(path, configPath), data)
}

func marshalServerRecord(host string, server Server) ([]byte, error) {
	plain, err := json.Marshal(serverRecord{Host: host, Port: server.Port, User: server.User, Key: server.Key})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server %q: %w", host, err)
	}
	return plain, nil
}

func parseServerEntries(data []byte) ([]serverEntry, error) {
	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	entries := make([]serverEntry, 0, len(cf.Servers))
	for i, cipher := range cf.Servers {
		plain, err := DecryptValue(cipher)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt server entry %d: %w", i, err)
		}
		var record serverRecord
		if err := json.Unmarshal(plain, &record); err != nil {
			return nil, fmt.Errorf("failed to parse server entry %d: %w", i, err)
		}
		entries = append(entries, serverEntry{cipher: cipher, plain: plain, record: record})
	}

	return entries, nil
}
