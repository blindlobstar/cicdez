package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
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

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "registry.example.com", "--username", "admin", "--password", "secret123"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry add failed: %v", err)
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

	output := buf.String()
	if !strings.Contains(output, "Login Succeeded") {
		t.Errorf("expected output to contain 'Login Succeeded', got: %s", output)
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

	cmd := NewRegistryCommandWithFactory(tokenFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"add", "gcr.io", "--username", "user", "--password", "pass"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry add failed: %v", err)
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

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "myregistry.com", "--username", "olduser", "--password", "oldpass"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry add failed: %v", err)
	}

	cmd = NewRegistryCommandWithFactory(mockClientFactory)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "myregistry.com", "--username", "newuser", "--password", "newpass"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("registry add (update) failed: %v", err)
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
		cmd := NewRegistryCommandWithFactory(mockClientFactory)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"add", server, "--username", r.username, "--password", r.password})
		err := cmd.Execute()
		if err != nil {
			t.Fatalf("registry add failed for %s: %v", server, err)
		}
	}

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry list failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Registries:") {
		t.Errorf("expected output to contain 'Registries:', got: %s", output)
	}
	for server := range registries {
		if !strings.Contains(output, server) {
			t.Errorf("expected output to contain '%s', got: %s", server, output)
		}
	}
}

func TestRegistryListEmpty(t *testing.T) {
	setupTestEnv(t)

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry list failed on empty registries: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No registries found") {
		t.Errorf("expected output to contain 'No registries found', got: %s", output)
	}
}

func TestRegistryRemove(t *testing.T) {
	setupTestEnv(t)

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "temp-registry.com", "--username", "tempuser", "--password", "temppass"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("registry add failed: %v", err)
	}

	cmd = NewRegistryCommandWithFactory(mockClientFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"remove", "temp-registry.com"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("registry remove failed: %v", err)
	}

	config, err := vault.LoadConfig(".")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if _, exists := config.Registries["temp-registry.com"]; exists {
		t.Error("expected temp-registry.com to be removed, but it still exists")
	}

	output := buf.String()
	if !strings.Contains(output, "Registry 'temp-registry.com' removed") {
		t.Errorf("expected output to contain success message, got: %s", output)
	}
}

func TestRegistryRemoveNonExistent(t *testing.T) {
	setupTestEnv(t)

	cmd := NewRegistryCommandWithFactory(mockClientFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "non-existent"})

	err := cmd.Execute()
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

	cmd := NewRegistryCommandWithFactory(errorFactory)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", "private.registry.com", "--username", "user", "--password", "wrongpass"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error on login failure, got nil")
	}
}
