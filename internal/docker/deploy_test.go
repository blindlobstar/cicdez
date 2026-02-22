package docker

import (
	"testing"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/compose-spec/compose-go/v2/types"
)

func TestProcessSensitiveSecrets_ExplicitTarget(t *testing.T) {
	project := types.Project{
		Services: types.Services{
			"web": types.ServiceConfig{
				Name: "web",
				Sensitive: map[string]types.SensitiveConfig{
					"my_secret": {
						Target: "/app/secrets/password",
						Format: "raw",
						Secrets: []types.SensitiveSecret{
							{Source: "db_password"},
						},
					},
				},
			},
		},
	}

	secrets := vault.Secrets{
		"db_password": "secret123",
	}

	err := processSensitiveSecrets(&project, secrets, "")
	if err != nil {
		t.Fatalf("processSensitiveSecrets failed: %v", err)
	}

	webService := project.Services["web"]
	if len(webService.Secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(webService.Secrets))
	}

	if webService.Secrets[0].Target != "/app/secrets/password" {
		t.Errorf("expected target '/app/secrets/password', got '%s'", webService.Secrets[0].Target)
	}
}
