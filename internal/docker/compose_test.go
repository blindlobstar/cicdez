package docker

import (
	"context"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestComposeParser(t *testing.T) {
	tests := []struct {
		name      string
		files     []string
		checkFunc func(*testing.T, types.Project)
	}{
		{
			name:  "Single compose file with all extensions",
			files: []string{"../../testdata/docker-compose.yml"},
			checkFunc: func(t *testing.T, project types.Project) {
				if len(project.Services) != 2 {
					t.Errorf("expected 2 services, got %d", len(project.Services))
				}

				webService, ok := project.Services["web"]
				if !ok {
					t.Fatal("web service not found")
				}

				if webService.Image != "myregistry.com/myapp:{git.tag}" {
					t.Errorf("expected image 'myregistry.com/myapp:{git.tag}', got '%s'", webService.Image)
				}

				if len(webService.Prebuild) != 1 {
					t.Fatalf("expected 1 prebuild job, got %d", len(webService.Prebuild))
				}

				job := webService.Prebuild[0]
				if job.Name != "Test and Lint" {
					t.Errorf("expected job name 'Test and Lint', got '%s'", job.Name)
				}

				if job.RunsOn != "node:18" {
					t.Errorf("expected runs-on 'node:18', got '%s'", job.RunsOn)
				}

				if len(job.Commands) != 3 {
					t.Fatalf("expected 3 commands, got %d", len(job.Commands))
				}

				if job.Commands[0].Name != "Install dependencies" {
					t.Errorf("expected first command name 'Install dependencies', got '%s'", job.Commands[0].Name)
				}

				if job.Commands[0].Command != "npm ci" {
					t.Errorf("expected first command 'npm ci', got '%s'", job.Commands[0].Command)
				}

				if len(webService.Sensitive) != 3 {
					t.Fatalf("expected 3 sensitive configs, got %d", len(webService.Sensitive))
				}

				templateConfig, ok := webService.Sensitive["app_config"]
				if !ok {
					t.Fatal("app_config sensitive config not found")
				}
				if templateConfig.Format != "template" {
					t.Errorf("expected format 'template', got '%s'", templateConfig.Format)
				}
				if templateConfig.Template != "./templates/config.yaml.tmpl" {
					t.Errorf("expected template './templates/config.yaml.tmpl', got '%s'", templateConfig.Template)
				}

				envConfig, ok := webService.Sensitive["app_env"]
				if !ok {
					t.Fatal("app_env sensitive config not found")
				}
				if envConfig.Target != "/app/.env" {
					t.Errorf("expected target '/app/.env', got '%s'", envConfig.Target)
				}

				if envConfig.Format != "env" {
					t.Errorf("expected format 'env', got '%s'", envConfig.Format)
				}

				if len(envConfig.Secrets) != 2 {
					t.Fatalf("expected 2 secrets, got %d", len(envConfig.Secrets))
				}

				if envConfig.Secrets[0].Source != "db_password" {
					t.Errorf("expected source 'db_password', got '%s'", envConfig.Secrets[0].Source)
				}

				if envConfig.Secrets[0].Name != "DB_PASSWORD" {
					t.Errorf("expected name 'DB_PASSWORD', got '%s'", envConfig.Secrets[0].Name)
				}

				if envConfig.UID != "1000" {
					t.Errorf("expected uid '1000', got '%s'", envConfig.UID)
				}

				if len(webService.LocalConfigs) != 2 {
					t.Fatalf("expected 2 local configs, got %d", len(webService.LocalConfigs))
				}

				nginxConfig, ok := webService.LocalConfigs["nginx_conf"]
				if !ok {
					t.Fatal("nginx_conf local config not found")
				}
				if nginxConfig.Source != "./configs/nginx.conf" {
					t.Errorf("expected source './configs/nginx.conf', got '%s'", nginxConfig.Source)
				}

				if nginxConfig.Target != "/etc/nginx/nginx.conf" {
					t.Errorf("expected target '/etc/nginx/nginx.conf', got '%s'", nginxConfig.Target)
				}

				dbService := project.Services["db"]
				if len(dbService.Sensitive) != 1 {
					t.Fatalf("expected 1 sensitive config for db, got %d", len(dbService.Sensitive))
				}

				rawConfig, ok := dbService.Sensitive["db_pass"]
				if !ok {
					t.Fatal("db_pass sensitive config not found")
				}
				if rawConfig.Format != "raw" {
					t.Errorf("expected format 'raw', got '%s'", rawConfig.Format)
				}

				if len(project.Networks) != 1 {
					t.Errorf("expected 1 network, got %d", len(project.Networks))
				}

				network, ok := project.Networks["app-network"]
				if !ok {
					t.Fatal("app-network not found")
				}

				if network.Driver != "overlay" {
					t.Errorf("expected driver 'overlay', got '%s'", network.Driver)
				}

				if len(project.Volumes) != 1 {
					t.Errorf("expected 1 volume, got %d", len(project.Volumes))
				}
			},
		},
		{
			name:  "Multiple compose files with override",
			files: []string{"../../testdata/docker-compose.yml", "../../testdata/docker-compose.override.yml"},
			checkFunc: func(t *testing.T, project types.Project) {
				webService, ok := project.Services["web"]
				if !ok {
					t.Fatal("web service not found")
				}

				debugValue, hasDebug := webService.Environment["DEBUG"]
				if !hasDebug {
					t.Error("expected DEBUG environment variable from override file")
				} else if *debugValue != "true" {
					t.Errorf("expected DEBUG=true, got DEBUG=%s", *debugValue)
				}

				if len(webService.Prebuild) == 0 {
					t.Error("prebuild should be preserved from base file")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			project, err := LoadCompose(ctx, []string{}, tt.files...)
			if err != nil {
				t.Fatalf("LoadCompose failed: %v", err)
			}

			tt.checkFunc(t, project)
		})
	}
}
