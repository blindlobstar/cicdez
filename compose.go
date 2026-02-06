package main

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	clitypes "github.com/docker/cli/cli/config/types"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/docker/docker/api/types/registry"
)

func loadCompose(ctx context.Context, env []string, paths ...string) (types.Project, error) {
	projectOptions, err := cli.NewProjectOptions(
		paths,
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithEnv(env),
		cli.WithInterpolation(true),
	)
	if err != nil {
		return types.Project{}, fmt.Errorf("failed to create project options: %w", err)
	}
	composeProject, err := projectOptions.LoadProject(ctx)
	if err != nil {
		return types.Project{}, fmt.Errorf("error to load project: %w", err)
	}

	return *composeProject, nil
}

func newComposeService(registries map[string]registry.AuthConfig) (api.Compose, error) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("failed to create docker cli: %w", err)
	}

	opts := cliflags.NewClientOptions()
	err = dockerCli.Initialize(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker cli: %w", err)
	}

	cfg := dockerCli.ConfigFile()
	if cfg.AuthConfigs == nil {
		cfg.AuthConfigs = make(map[string]clitypes.AuthConfig)
	}
	for host, auth := range registries {
		cfg.AuthConfigs[host] = clitypes.AuthConfig{
			Username:      auth.Username,
			Password:      auth.Password,
			Auth:          auth.Auth,
			IdentityToken: auth.IdentityToken,
			ServerAddress: auth.ServerAddress,
		}
	}

	return compose.NewComposeService(dockerCli), nil
}
