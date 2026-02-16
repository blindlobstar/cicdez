package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vrotherford/cicdez/internal/vault"
)

func TestServerAdd(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		host: "192.168.1.100",
		user: "deploy",
	}

	err := runServerAdd(nil, []string{"production"}, opts)
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

	if server.User != "deploy" {
		t.Errorf("expected user 'deploy', got '%s'", server.User)
	}
}

func TestServerAddWithKeyFile(t *testing.T) {
	setupTestEnv(t)

	// Create temp key file
	keyContent := "-----BEGIN PRIVATE KEY-----\ntest_key\n-----END PRIVATE KEY-----"
	keyFile := filepath.Join(t.TempDir(), "test_key")
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	opts := &serverAddOptions{
		host:    "10.0.0.5",
		user:    "ubuntu",
		keyFile: keyFile,
	}

	err := runServerAdd(nil, []string{"staging"}, opts)
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
		host: "old-host.example.com",
		user: "olduser",
	}

	err := runServerAdd(nil, []string{"myserver"}, opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	// Create temp key file for update
	keyContent := "new_key"
	keyFile := filepath.Join(t.TempDir(), "new_key")
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	opts = &serverAddOptions{
		host:    "new-host.example.com",
		user:    "newuser",
		keyFile: keyFile,
	}

	err = runServerAdd(nil, []string{"myserver"}, opts)
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
			host: s.host,
			user: s.user,
		}
		err := runServerAdd(nil, []string{name}, opts)
		if err != nil {
			t.Fatalf("runServerAdd failed for %s: %v", name, err)
		}
	}

	err := runServerList(nil, nil)
	if err != nil {
		t.Fatalf("runServerList failed: %v", err)
	}
}

func TestServerListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runServerList(nil, nil)
	if err != nil {
		t.Fatalf("runServerList failed on empty servers: %v", err)
	}
}

func TestServerRemove(t *testing.T) {
	setupTestEnv(t)

	opts := &serverAddOptions{
		host: "temp.example.com",
		user: "temp",
	}

	err := runServerAdd(nil, []string{"temp-server"}, opts)
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	err = runServerRemove(nil, []string{"temp-server"})
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

	err := runServerRemove(nil, []string{"non-existent"})
	if err == nil {
		t.Error("expected error when removing non-existent server, got nil")
	}
}
