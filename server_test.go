package main

import (
	"testing"
)

func TestServerAdd(t *testing.T) {
	setupTestEnv(t)

	serverHost = "192.168.1.100"
	serverUser = "deploy"
	serverKey = ""

	err := runServerAdd(nil, []string{"production"})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
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

func TestServerAddWithKey(t *testing.T) {
	setupTestEnv(t)

	serverHost = "10.0.0.5"
	serverUser = "ubuntu"
	serverKey = "-----BEGIN PRIVATE KEY-----\ntest_key\n-----END PRIVATE KEY-----"

	err := runServerAdd(nil, []string{"staging"})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	server, exists := config.Servers["staging"]
	if !exists {
		t.Error("expected staging server to exist")
	}

	if server.Key == "" {
		t.Error("expected server to have SSH key configured")
	}
}

func TestServerAddUpdate(t *testing.T) {
	setupTestEnv(t)

	serverHost = "old-host.example.com"
	serverUser = "olduser"
	serverKey = ""

	err := runServerAdd(nil, []string{"myserver"})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	serverHost = "new-host.example.com"
	serverUser = "newuser"
	serverKey = "new_key"

	err = runServerAdd(nil, []string{"myserver"})
	if err != nil {
		t.Fatalf("runServerAdd (update) failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	server := config.Servers["myserver"]
	if server.Host != "new-host.example.com" {
		t.Errorf("expected host 'new-host.example.com', got '%s'", server.Host)
	}

	if server.User != "newuser" {
		t.Errorf("expected user 'newuser', got '%s'", server.User)
	}

	if server.Key != "new_key" {
		t.Errorf("expected key 'new_key', got '%s'", server.Key)
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
		serverHost = s.host
		serverUser = s.user
		serverKey = ""
		err := runServerAdd(nil, []string{name})
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

	serverHost = "temp.example.com"
	serverUser = "temp"
	serverKey = ""

	err := runServerAdd(nil, []string{"temp-server"})
	if err != nil {
		t.Fatalf("runServerAdd failed: %v", err)
	}

	err = runServerRemove(nil, []string{"temp-server"})
	if err != nil {
		t.Fatalf("runServerRemove failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
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
