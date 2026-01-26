package main

import (
	"testing"
)

func TestRegistryAdd(t *testing.T) {
	setupTestEnv(t)

	registryURL = "https://registry.example.com"
	registryUsername = "admin"
	registryPassword = "secret123"

	err := runRegistryAdd(nil, []string{"production"})
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	registry, exists := config.Registries["production"]
	if !exists {
		t.Error("expected production registry to exist")
	}

	if registry.URL != "https://registry.example.com" {
		t.Errorf("expected URL 'https://registry.example.com', got '%s'", registry.URL)
	}

	if registry.Username != "admin" {
		t.Errorf("expected username 'admin', got '%s'", registry.Username)
	}

	if registry.Password != "secret123" {
		t.Errorf("expected password 'secret123', got '%s'", registry.Password)
	}
}

func TestRegistryAddUpdate(t *testing.T) {
	setupTestEnv(t)

	registryURL = "https://old-registry.com"
	registryUsername = "olduser"
	registryPassword = "oldpass"

	err := runRegistryAdd(nil, []string{"myregistry"})
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	registryURL = "https://new-registry.com"
	registryUsername = "newuser"
	registryPassword = "newpass"

	err = runRegistryAdd(nil, []string{"myregistry"})
	if err != nil {
		t.Fatalf("runRegistryAdd (update) failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	registry := config.Registries["myregistry"]
	if registry.URL != "https://new-registry.com" {
		t.Errorf("expected URL 'https://new-registry.com', got '%s'", registry.URL)
	}

	if registry.Username != "newuser" {
		t.Errorf("expected username 'newuser', got '%s'", registry.Username)
	}

	if registry.Password != "newpass" {
		t.Errorf("expected password 'newpass', got '%s'", registry.Password)
	}
}

func TestRegistryList(t *testing.T) {
	setupTestEnv(t)

	registries := map[string]struct {
		url      string
		username string
		password string
	}{
		"docker": {"https://hub.docker.com", "dockeruser", "dockerpass"},
		"gcr":    {"https://gcr.io", "gcruser", "gcrpass"},
		"ecr":    {"https://ecr.aws.com", "ecruser", "ecrpass"},
	}

	for name, r := range registries {
		registryURL = r.url
		registryUsername = r.username
		registryPassword = r.password
		err := runRegistryAdd(nil, []string{name})
		if err != nil {
			t.Fatalf("runRegistryAdd failed for %s: %v", name, err)
		}
	}

	err := runRegistryList(nil, nil)
	if err != nil {
		t.Fatalf("runRegistryList failed: %v", err)
	}
}

func TestRegistryListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runRegistryList(nil, nil)
	if err != nil {
		t.Fatalf("runRegistryList failed on empty registries: %v", err)
	}
}

func TestRegistryRemove(t *testing.T) {
	setupTestEnv(t)

	registryURL = "https://temp-registry.com"
	registryUsername = "tempuser"
	registryPassword = "temppass"

	err := runRegistryAdd(nil, []string{"temp-registry"})
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	err = runRegistryRemove(nil, []string{"temp-registry"})
	if err != nil {
		t.Fatalf("runRegistryRemove failed: %v", err)
	}

	e, err := NewEncrypter(".")
	if err != nil {
		t.Fatalf("NewEncrypter failed: %v", err)
	}

	config, err := loadConfig(e, ".")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if _, exists := config.Registries["temp-registry"]; exists {
		t.Error("expected temp-registry to be removed, but it still exists")
	}
}

func TestRegistryRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runRegistryRemove(nil, []string{"non-existent"})
	if err == nil {
		t.Error("expected error when removing non-existent registry, got nil")
	}
}
