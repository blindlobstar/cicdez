package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/blindlobstar/cicdez/internal/docker"
	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

type deployOptions struct {
	composeFiles []string
	stack        string
	prune        bool
	resolveImage string
	quiet        bool
	noBuild      bool
	noCache      bool
	pull         bool
	server       string
}

func NewDeployCommand() *cobra.Command {
	opts := deployOptions{}
	cmd := &cobra.Command{
		Use:   "deploy [stack]",
		Short: "Deploy a stack to Docker Swarm",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.stack = args[0]
			}
			return runDeploy(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringArrayVarP(&opts.composeFiles, "file", "f", []string{"compose.yaml"}, "Compose file path(s)")
	cmd.Flags().BoolVar(&opts.prune, "prune", false, "Prune services that are no longer referenced")
	cmd.Flags().StringVar(&opts.resolveImage, "resolve-image", docker.ResolveImageAlways, "Query the registry to resolve image digest and supported platforms (\"always\", \"changed\", \"never\")")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress progress output")
	cmd.Flags().BoolVar(&opts.noBuild, "no-build", false, "Skip building images before deploy")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Do not use cache when building")
	cmd.Flags().BoolVar(&opts.pull, "pull", false, "Always pull newer versions of base images")
	cmd.Flags().StringVar(&opts.server, "server", "", "Server to deploy to (uses default if not specified)")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := vault.LoadConfig(cwd)
	if err != nil {
		return err
	}

	project, err := docker.LoadCompose(ctx, os.Environ(), opts.composeFiles...)
	if err != nil {
		return err
	}

	if opts.stack == "" {
		// compose-go defaults project.Name to the directory name if not set
		opts.stack = project.Name
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if !opts.noBuild && docker.HasBuildConfig(project) {
		dockerClient, err := client.New(client.WithHostFromEnv())
		if err != nil {
			return fmt.Errorf("failed to create local docker client: %w", err)
		}
		defer dockerClient.Close()

		buildOpts := docker.BuildOptions{
			Cwd:        cwd,
			Registries: cfg.Registries,
			NoCache:    opts.noCache,
			Pull:       opts.pull,
			Push:       true,
		}

		if err := docker.Build(ctx, dockerClient, project, buildOpts); err != nil {
			return fmt.Errorf("failed to build and push images: %w", err)
		}
	}

	server, err := cfg.GetServer(opts.server)
	if err != nil {
		return err
	}

	dockerClient, err := docker.NewClientSSH(server.Host, server.Port, server.User, []byte(server.Key))
	if err != nil {
		return err
	}

	err = docker.Deploy(ctx, dockerClient, project, docker.DeployOptions{
		Cwd:          cwd,
		Secrets:      secrets,
		Stack:        opts.stack,
		Prune:        opts.prune,
		ResolveImage: opts.resolveImage,
		Quiet:        opts.quiet,
		Registries:   cfg.Registries,
	})
	if err != nil {
		return err
	}

	return nil
}
