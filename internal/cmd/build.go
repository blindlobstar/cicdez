package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
	"github.com/blindlobstar/cicdez/internal/docker"
	"github.com/blindlobstar/cicdez/internal/vault"
)

type buildOptions struct {
	composeFile string
	services    []string
	noCache     bool
	pull        bool
	push        bool
}

func NewBuildCommand() *cobra.Command {
	opts := buildOptions{}
	cmd := &cobra.Command{
		Use:   "build [SERVICE...]",
		Short: "Build images from compose file",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.services = args
			return runBuild(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVarP(&opts.composeFile, "file", "f", "compose.yaml", "compose file path")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "do not use cache when building")
	cmd.Flags().BoolVar(&opts.pull, "pull", false, "pull newer versions of base images")
	cmd.Flags().BoolVar(&opts.push, "push", false, "push images after build")
	return cmd
}

func runBuild(ctx context.Context, opts buildOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	project, err := docker.LoadCompose(ctx, nil, opts.composeFile)
	if err != nil {
		return fmt.Errorf("failed to load compose file: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	dockerClient, err := client.New(client.WithHostFromEnv())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	servicesToBuild := make(map[string]bool)
	for _, svc := range opts.services {
		servicesToBuild[svc] = true
	}

	buildOpts := docker.BuildOptions{
		Services:   servicesToBuild,
		Cwd:        cwd,
		Registries: config.Registries,
		NoCache:    opts.noCache,
		Pull:       opts.pull,
		Push:       opts.push,
	}

	return docker.Build(ctx, dockerClient, project, buildOpts)
}
