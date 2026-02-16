package cmd

import (
	"fmt"
	"os"

	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
	"github.com/vrotherford/cicdez/internal/docker"
	"github.com/vrotherford/cicdez/internal/vault"
)

type buildCommandOptions struct {
	composeFile string
	noCache     bool
	pull        bool
	push        bool
}

func NewBuildCommand() *cobra.Command {
	opts := &buildCommandOptions{}
	cmd := &cobra.Command{
		Use:   "build [services...]",
		Short: "Build images from compose file",
		Long:  "Build Docker images for services defined in compose file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd, args, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.composeFile, "file", "f", "compose.yaml", "Compose file path")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Do not use cache when building")
	cmd.Flags().BoolVar(&opts.pull, "pull", false, "Always pull newer versions of base images")
	cmd.Flags().BoolVar(&opts.push, "push", false, "Push images after build")
	return cmd
}

func runBuild(cmd *cobra.Command, args []string, cmdOpts *buildCommandOptions) error {
	ctx := cmd.Context()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	project, err := docker.LoadCompose(ctx, nil, cmdOpts.composeFile)
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
	for _, arg := range args {
		servicesToBuild[arg] = true
	}

	opts := docker.BuildOptions{
		Services:   servicesToBuild,
		Cwd:        cwd,
		Registries: config.Registries,
		NoCache:    cmdOpts.noCache,
		Pull:       cmdOpts.pull,
		Push:       cmdOpts.push,
	}

	return docker.Build(ctx, dockerClient, project, opts)
}
