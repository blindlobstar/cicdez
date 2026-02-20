package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blindlobstar/cicdez/internal/vault"
)

func TestServerAdd(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		name: "production",
		host: "192.168.1.100",
		user: "deploy",
	}

	err := runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	server, exists := config.Servers["production"]
	if !exists {
		t.Error("expected production server to exist")
	}

	if server.Host != "192.168.1.100" {
		t.Errorf("expected host '192.168.1.100', got '%s'", server.Host)
	}

	if server.Port != 22 {
		t.Errorf("expected port 22, got %d", server.Port)
	}

	if server.User != "deploy" {
		t.Errorf("expected user 'deploy', got '%s'", server.User)
	}
}

func TestServerAddDefaultUser(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		name: "prod",
		host: "192.168.1.10",
		user: "root",
	}

	err := runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	server, exists := config.Servers["prod"]
	if !exists {
		t.Error("expected prod server to exist")
	}

	if server.User != "root" {
		t.Errorf("expected user 'root', got '%s'", server.User)
	}
}

func TestServerAddWithPort(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		name: "staging",
		host: "192.168.1.10:2222",
		user: "deploy",
	}

	err := runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	server, exists := config.Servers["staging"]
	if !exists {
		t.Error("expected staging server to exist")
	}

	if server.Host != "192.168.1.10" {
		t.Errorf("expected host '192.168.1.10', got '%s'", server.Host)
	}

	if server.Port != 2222 {
		t.Errorf("expected port 2222, got %d", server.Port)
	}
}

func TestServerAddWithKeyFile(t *testing.T) {
	setupTestEnv(t)

	keyContent := "-----BEGIN PRIVATE KEY-----\ntest_key\n-----END PRIVATE KEY-----"
	keyFile := filepath.Join(t.TempDir(), "test_key")
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	opts := &serverAddOptions{
		name:    "staging",
		host:    "10.0.0.5",
		user:    "ubuntu",
		keyFile: keyFile,
	}

	err := runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	server, exists := config.Servers["staging"]
	if !exists {
		t.Error("expected staging server to exist")
	}

	if server.Key != keyContent {
		t.Errorf("expected server key to match file content, got '%s'", server.Key)
	}
}

func TestServerAddUpdate(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		name: "myserver",
		host: "old-host.example.com",
		user: "olduser",
	}

	err := runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	keyContent := "new_key"
	keyFile := filepath.Join(t.TempDir(), "new_key")
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	opts = &serverAddOptions{
		name:    "myserver",
		host:    "new-host.example.com",
		user:    "newuser",
		keyFile: keyFile,
	}

	err = runServerAdd(opts)
	if err != nil {
		t.Fatalf("runServerAdd (update) failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	server := config.Servers["myserver"]
	if server.Host != "new-host.example.com" {
		t.Errorf("expected host 'new-host.example.com', got '%s'", server.Host)
	}

	if server.User != "newuser" {
		t.Errorf("expected user 'newuser', got '%s'", server.User)
	}

	if server.Key != keyContent {
		t.Errorf("expected key '%s', got '%s'", keyContent, server.Key)
	}
}

func TestServerList(t *testing.T) {
	setupTestEnv(t)

	servers := map[string]struct {
		host string
		user string
	}{
		"prod1": {"prod1.example.com", "deploy"},
		"prod2": {"prod2.example.com", "deploy"},
		"dev":   {"dev.example.com", "developer"},
	}

	for name, s := range servers {
		opts := &serverAddOptions{
			name: name,
			host: s.host,
			user: s.user,
		}
		err := runServerAdd(opts)
		if err != nil {
			t.Fatalf("runServerAdd failed for %s: %v", name, err)
		}
	}

	err := runServerList()
	if err != nil {
		t.Fatalf("runServerList failed: %v", err)
	}
}

func TestServerListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runServerList()
	if err != nil {
		t.Fatalf("runServerList failed on empty servers: %v", err)
	}
}

func TestServerRemove(t *testing.T) {
	setupTestEnv(t)

	err := runServerAdd(&serverAddOptions{
		name: "temp-server",
		host: "temp.example.com",
		user: "temp",
	})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	err = runServerRemove(&serverRemoveOptions{name: "temp-server"})
	if err != nil {
		t.Fatalf("runServerRemove failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if _, exists := config.Servers["temp-server"]; exists {
		t.Error("expected temp-server to be removed, but it still exists")
	}
}

func TestServerRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runServerRemove(&serverRemoveOptions{name: "non-existent"})
	if err == nil {
		t.Error("expected error when removing non-existent server, got nil")
	}
}

func TestServerAddFirstIsDefault(t *testing.T) {
	setupTestEnv(t)

	err := runServerAdd(&serverAddOptions{
		name: "first",
		host: "first.example.com",
		user: "deploy",
	})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "first" {
		t.Errorf("expected default server 'first', got '%s'", config.DefaultServer)
	}

	err = runServerAdd(&serverAddOptions{
		name: "second",
		host: "second.example.com",
		user: "deploy",
	})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	config, err = vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "first" {
		t.Errorf("expected default server to remain 'first', got '%s'", config.DefaultServer)
	}
}

func TestServerSetDefault(t *testing.T) {
	setupTestEnv(t)

	for _, name := range []string{"server1", "server2"} {
		err := runServerAdd(&serverAddOptions{
			name: name,
			host: name + ".example.com",
			user: "deploy",
		})
		if err != nil {
			t.Fatalf("runServerAdd failed: %v", err)
		}
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "server1" {
		t.Errorf("expected default server 'server1', got '%s'", config.DefaultServer)
	}

	err = runServerSetDefault(&serverSetDefaultOptions{name: "server2"})
	if err != nil {
		t.Fatalf("runServerSetDefault failed: %v", err)
	}

	config, err = vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "server2" {
		t.Errorf("expected default server 'server2', got '%s'", config.DefaultServer)
	}
}

func TestServerSetDefaultNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runServerSetDefault(&serverSetDefaultOptions{name: "non-existent"})
	if err == nil {
		t.Error("expected error when setting non-existent server as default, got nil")
	}
}

func TestServerRemoveReassignsDefault(t *testing.T) {
	setupTestEnv(t)

	for _, name := range []string{"primary", "secondary"} {
		err := runServerAdd(&serverAddOptions{
			name: name,
			host: name + ".example.com",
			user: "deploy",
		})
		if err != nil {
			t.Fatalf("runServerAdd failed: %v", err)
		}
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "primary" {
		t.Errorf("expected default server 'primary', got '%s'", config.DefaultServer)
	}

	err = runServerRemove(&serverRemoveOptions{name: "primary"})
	if err != nil {
		t.Fatalf("runServerRemove failed: %v", err)
	}

	config, err = vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "secondary" {
		t.Errorf("expected default server to be reassigned to 'secondary', got '%s'", config.DefaultServer)
	}
}

func TestServerRemoveLastClearsDefault(t *testing.T) {
	setupTestEnv(t)

	err := runServerAdd(&serverAddOptions{
		name: "only",
		host: "only.example.com",
		user: "deploy",
	})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	err = runServerRemove(&serverRemoveOptions{name: "only"})
	if err != nil {
		t.Fatalf("runServerRemove failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "" {
		t.Errorf("expected default server to be empty, got '%s'", config.DefaultServer)
	}
}
