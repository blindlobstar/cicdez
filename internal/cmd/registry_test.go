package cmd

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/blindlobstar/cicdez/internal/vault"
)

type mockRegistryClient struct {
	loginFunc func(ctx context.Context, opts client.RegistryLoginOptions) (client.RegistryLoginResult, error)
}

func (m *mockRegistryClient) RegistryLogin(ctx context.Context, opts client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, opts)
	}
	return client.RegistryLoginResult{Auth: registry.AuthResponse{Status: "Login Succeeded"}}, nil
}

func mockClientFactory() (RegistryClient, error) {
	return &mockRegistryClient{}, nil
}

func TestRegistryAdd(t *testing.T) {
	setupTestEnv(t)

	opts := registryAddOptions{
		server:        "registry.example.com",
		username:      "admin",
		password:      "secret123",
		clientFactory: mockClientFactory,
	}

	err := runRegistryAdd(context.Background(), opts)
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	reg, exists := config.Registries["registry.example.com"]
	if !exists {
		t.Error("expected registry.example.com to exist")
	}

	if reg.ServerAddress != "registry.example.com" {
		t.Errorf("expected ServerAddress 'registry.example.com', got '%s'", reg.ServerAddress)
	}

	if reg.Username != "admin" {
		t.Errorf("expected username 'admin', got '%s'", reg.Username)
	}

	if reg.Password != "secret123" {
		t.Errorf("expected password 'secret123', got '%s'", reg.Password)
	}
}

func TestRegistryAddWithIdentityToken(t *testing.T) {
	setupTestEnv(t)

	tokenFactory := func() (RegistryClient, error) {
		return &mockRegistryClient{
			loginFunc: func(ctx context.Context, opts client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
				return client.RegistryLoginResult{
					Auth: registry.AuthResponse{
						Status:        "Login Succeeded",
						IdentityToken: "token123",
					},
				}, nil
			},
		}, nil
	}

	opts := registryAddOptions{
		server:        "gcr.io",
		username:      "user",
		password:      "pass",
		clientFactory: tokenFactory,
	}

	err := runRegistryAdd(context.Background(), opts)
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	reg := config.Registries["gcr.io"]
	if reg.IdentityToken != "token123" {
		t.Errorf("expected IdentityToken 'token123', got '%s'", reg.IdentityToken)
	}

	if reg.Password != "" {
		t.Errorf("expected Password to be cleared when IdentityToken is set, got '%s'", reg.Password)
	}
}

func TestRegistryAddUpdate(t *testing.T) {
	setupTestEnv(t)

	opts := registryAddOptions{
		server:        "myregistry.com",
		username:      "olduser",
		password:      "oldpass",
		clientFactory: mockClientFactory,
	}

	err := runRegistryAdd(context.Background(), opts)
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	opts = registryAddOptions{
		server:        "myregistry.com",
		username:      "newuser",
		password:      "newpass",
		clientFactory: mockClientFactory,
	}

	err = runRegistryAdd(context.Background(), opts)
	if err != nil {
		t.Fatalf("runRegistryAdd (update) failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	reg := config.Registries["myregistry.com"]
	if reg.Username != "newuser" {
		t.Errorf("expected username 'newuser', got '%s'", reg.Username)
	}

	if reg.Password != "newpass" {
		t.Errorf("expected password 'newpass', got '%s'", reg.Password)
	}
}

func TestRegistryList(t *testing.T) {
	setupTestEnv(t)

	registries := map[string]struct {
		username string
		password string
	}{
		"hub.docker.com": {"dockeruser", "dockerpass"},
		"gcr.io":         {"gcruser", "gcrpass"},
		"ecr.aws.com":    {"ecruser", "ecrpass"},
	}

	for server, r := range registries {
		opts := registryAddOptions{
			server:        server,
			username:      r.username,
			password:      r.password,
			clientFactory: mockClientFactory,
		}
		err := runRegistryAdd(context.Background(), opts)
		if err != nil {
			t.Fatalf("runRegistryAdd failed for %s: %v", server, err)
		}
	}

	err := runRegistryList()
	if err != nil {
		t.Fatalf("runRegistryList failed: %v", err)
	}
}

func TestRegistryListEmpty(t *testing.T) {
	setupTestEnv(t)

	err := runRegistryList()
	if err != nil {
		t.Fatalf("runRegistryList failed on empty registries: %v", err)
	}
}

func TestRegistryRemove(t *testing.T) {
	setupTestEnv(t)

	err := runRegistryAdd(context.Background(), registryAddOptions{
		server:        "temp-registry.com",
		username:      "tempuser",
		password:      "temppass",
		clientFactory: mockClientFactory,
	})
	if err != nil {
		t.Fatalf("runRegistryAdd failed: %v", err)
	}

	err = runRegistryRemove(registryRemoveOptions{server: "temp-registry.com"})
	if err != nil {
		t.Fatalf("runRegistryRemove failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if _, exists := config.Registries["temp-registry.com"]; exists {
		t.Error("expected temp-registry.com to be removed, but it still exists")
	}
}

func TestRegistryRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	err := runRegistryRemove(registryRemoveOptions{server: "non-existent"})
	if err == nil {
		t.Error("expected error when removing non-existent registry, got nil")
	}
}

func TestRegistryLoginError(t *testing.T) {
	setupTestEnv(t)

	errorFactory := func() (RegistryClient, error) {
		return &mockRegistryClient{
			loginFunc: func(ctx context.Context, opts client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
				return client.RegistryLoginResult{}, context.DeadlineExceeded
			},
		}, nil
	}

	opts := registryAddOptions{
		server:        "private.registry.com",
		username:      "user",
		password:      "wrongpass",
		clientFactory: errorFactory,
	}

	err := runRegistryAdd(context.Background(), opts)
	if err == nil {
		t.Error("expected error on login failure, got nil")
	}
}
