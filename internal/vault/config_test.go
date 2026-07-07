package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := setupTestKey(t)

	want := Config{Servers: map[string]Server{
		"203.0.113.1": {Port: 22, User: "cicdez", Key: PrivateKey("key-one")},
		"203.0.113.2": {Port: 2222, User: "deploy", Key: PrivateKey("key-two")},
	}}

	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(got.Servers) != len(want.Servers) {
		t.Fatalf("expected %d servers, got %d", len(want.Servers), len(got.Servers))
	}
	for host, server := range want.Servers {
		g, ok := got.Servers[host]
		if !ok {
			t.Fatalf("server %q missing after round trip", host)
		}
		if g.Port != server.Port || g.User != server.User || !bytes.Equal(g.Key, server.Key) {
			t.Errorf("server %q mismatch: got %+v, want %+v", host, g, server)
		}
	}
}

func TestConfigFileHidesHosts(t *testing.T) {
	dir := setupTestKey(t)

	config := Config{Servers: map[string]Server{
		"203.0.113.1": {Port: 22, User: "cicdez", Key: PrivateKey("super-private-key")},
	}}
	if err := SaveConfig(dir, config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, configPath))
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	for _, sensitive := range []string{"203.0.113.1", "cicdez", "super-private-key"} {
		if bytes.Contains(data, []byte(sensitive)) {
			t.Errorf("config file contains %q in plaintext", sensitive)
		}
	}

	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		t.Fatalf("failed to parse config file: %v", err)
	}
	for i, entry := range cf.Servers {
		if !strings.HasPrefix(entry, valuePrefix) {
			t.Errorf("server entry %d is not an encrypted value", i)
		}
	}
}

func TestSaveConfigPreservesCiphertext(t *testing.T) {
	dir := setupTestKey(t)

	config := Config{Servers: map[string]Server{
		"203.0.113.1": {Port: 22, User: "keep", Key: PrivateKey("key-one")},
		"203.0.113.2": {Port: 22, User: "old", Key: PrivateKey("key-two")},
	}}
	if err := SaveConfig(dir, config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	readEntries := func() map[string]struct {
		index  int
		cipher string
	} {
		data, err := os.ReadFile(filepath.Join(dir, configPath))
		if err != nil {
			t.Fatalf("failed to read config file: %v", err)
		}
		entries, err := parseServerEntries(data)
		if err != nil {
			t.Fatalf("failed to parse config file: %v", err)
		}
		byHost := make(map[string]struct {
			index  int
			cipher string
		}, len(entries))
		for i, e := range entries {
			byHost[e.record.Host] = struct {
				index  int
				cipher string
			}{i, e.cipher}
		}
		return byHost
	}

	before := readEntries()

	config.Servers["203.0.113.2"] = Server{Port: 22, User: "new", Key: PrivateKey("key-two")}
	config.Servers["203.0.113.3"] = Server{Port: 22, User: "added", Key: PrivateKey("key-three")}
	if err := SaveConfig(dir, config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	after := readEntries()

	if after["203.0.113.1"].cipher != before["203.0.113.1"].cipher {
		t.Error("expected unchanged server to keep its ciphertext")
	}
	if after["203.0.113.1"].index != before["203.0.113.1"].index {
		t.Error("expected unchanged server to keep its position")
	}
	if after["203.0.113.2"].cipher == before["203.0.113.2"].cipher {
		t.Error("expected changed server to get new ciphertext")
	}
	if after["203.0.113.2"].index != before["203.0.113.2"].index {
		t.Error("expected changed server to keep its position")
	}
	if added, ok := after["203.0.113.3"]; !ok {
		t.Error("expected added server to be present")
	} else if added.index != len(after)-1 {
		t.Error("expected added server to be appended at the end")
	}
}

func TestLoadConfigDuplicateHostLastWins(t *testing.T) {
	dir := setupTestKey(t)

	first, err := marshalServerRecord("203.0.113.1", Server{Port: 22, User: "old", Key: PrivateKey("key-old")})
	if err != nil {
		t.Fatalf("failed to marshal record: %v", err)
	}
	second, err := marshalServerRecord("203.0.113.1", Server{Port: 22, User: "new", Key: PrivateKey("key-new")})
	if err != nil {
		t.Fatalf("failed to marshal record: %v", err)
	}

	var cf configFile
	for _, plain := range [][]byte{first, second} {
		cipher, err := EncryptValue(plain)
		if err != nil {
			t.Fatalf("failed to encrypt record: %v", err)
		}
		cf.Servers = append(cf.Servers, cipher)
	}
	data, err := yaml.Marshal(cf)
	if err != nil {
		t.Fatalf("failed to marshal config file: %v", err)
	}
	if err := writeVaultFile(filepath.Join(dir, configPath), data); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	config, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(config.Servers) != 1 {
		t.Fatalf("expected 1 server after dedup, got %d", len(config.Servers))
	}
	if config.Servers["203.0.113.1"].User != "new" {
		t.Errorf("expected last duplicate to win, got user %q", config.Servers["203.0.113.1"].User)
	}

	// saving collapses the duplicate lines
	if err := SaveConfig(dir, config); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configPath))
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var saved configFile
	if err := yaml.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("failed to parse config file: %v", err)
	}
	if len(saved.Servers) != 1 {
		t.Errorf("expected 1 entry after save, got %d", len(saved.Servers))
	}
}

func TestLoadConfigMissing(t *testing.T) {
	dir := setupTestKey(t)

	config, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig failed on missing file: %v", err)
	}
	if len(config.Servers) != 0 {
		t.Errorf("expected no servers, got %d", len(config.Servers))
	}
}
