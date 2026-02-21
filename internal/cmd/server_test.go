package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blindlobstar/cicdez/internal/vault"
)

func TestServerAdd(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "production", "--host", "192.168.1.100", "--user", "deploy"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
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

	output := buf.String()
	if !strings.Contains(output, "Server 'production' added") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
}

func TestServerAddDefaultUser(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "prod", "--host", "192.168.1.10"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
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

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "staging", "--host", "192.168.1.10:2222", "--user", "deploy"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
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

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "staging", "--host", "10.0.0.5", "--user", "ubuntu", "--key-file", keyFile})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
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

	cmd := NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "myserver", "--host", "old-host.example.com", "--user", "olduser"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
	}

	keyContent := "new_key"
	keyFile := filepath.Join(t.TempDir(), "new_key")
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	cmd = NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "myserver", "--host", "new-host.example.com", "--user", "newuser", "--key-file", keyFile})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server add (update) failed: %v", err)
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
		cmd := NewServerCommand()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"add", name, "--host", s.host, "--user", s.user})
		err := cmd.Execute()
		if err != nil {
			t.Fatalf("server add failed for %s: %v", name, err)
		}
	}

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server list failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Servers:") {
		t.Errorf("expected output to contain 'Servers:', got: %s", output)
	}
	for name := range servers {
		if !strings.Contains(output, name) {
			t.Errorf("expected output to contain '%s', got: %s", name, output)
		}
	}
}

func TestServerListEmpty(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server list failed on empty servers: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No servers found") {
		t.Errorf("expected output to contain 'No servers found', got: %s", output)
	}
}

func TestServerRemove(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "temp-server", "--host", "temp.example.com", "--user", "temp"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
	}

	cmd = NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"remove", "temp-server"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server remove failed: %v", err)
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

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "non-existent"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when removing non-existent server, got nil")
	}
}

func TestServerAddFirstIsDefault(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "first", "--host", "first.example.com", "--user", "deploy"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "first" {
		t.Errorf("expected default server 'first', got '%s'", config.DefaultServer)
	}

	cmd = NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "second", "--host", "second.example.com", "--user", "deploy"})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
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
		cmd := NewServerCommand()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"add", name, "--host", name + ".example.com", "--user", "deploy"})
		err := cmd.Execute()
		if err != nil {
			t.Fatalf("server add failed: %v", err)
		}
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "server1" {
		t.Errorf("expected default server 'server1', got '%s'", config.DefaultServer)
	}

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set-default", "server2"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server set-default failed: %v", err)
	}

	config, err = vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "server2" {
		t.Errorf("expected default server 'server2', got '%s'", config.DefaultServer)
	}

	output := buf.String()
	if !strings.Contains(output, "Server 'server2' set as default") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
}

func TestServerSetDefaultNonExistent(t *testing.T) {
	setupTestEnv(t)

	cmd := NewServerCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"set-default", "non-existent"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when setting non-existent server as default, got nil")
	}
}

func TestServerRemoveReassignsDefault(t *testing.T) {
	setupTestEnv(t)

	for _, name := range []string{"primary", "secondary"} {
		cmd := NewServerCommand()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"add", name, "--host", name + ".example.com", "--user", "deploy"})
		err := cmd.Execute()
		if err != nil {
			t.Fatalf("server add failed: %v", err)
		}
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "primary" {
		t.Errorf("expected default server 'primary', got '%s'", config.DefaultServer)
	}

	cmd := NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"remove", "primary"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server remove failed: %v", err)
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

	cmd := NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "only", "--host", "only.example.com", "--user", "deploy"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("server add failed: %v", err)
	}

	cmd = NewServerCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"remove", "only"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("server remove failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.DefaultServer != "" {
		t.Errorf("expected default server to be empty, got '%s'", config.DefaultServer)
	}
}
