package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

type deployCommandOptions struct {
	composeFiles []string
	prune        bool
	resolveImage string
	detach       bool
	quiet        bool
	noBuild      bool
	noCache      bool
	pull         bool
}

func newDeployCommand() *cobra.Command {
	opts := &deployCommandOptions{}
	cmd := &cobra.Command{
		Use:   "deploy [stack]",
		Short: "Deploy a stack to Docker Swarm",
		Long:  "Deploy services defined in compose file to Docker Swarm cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeployCommand(cmd, args, opts)
		},
	}
	cmd.Flags().StringArrayVarP(&opts.composeFiles, "file", "f", []string{"compose.yaml"}, "Compose file path(s)")
	cmd.Flags().BoolVar(&opts.prune, "prune", false, "Prune services that are no longer referenced")
	cmd.Flags().StringVar(&opts.resolveImage, "resolve-image", resolveImageAlways, "Query the registry to resolve image digest and supported platforms (\"always\", \"changed\", \"never\")")
	cmd.Flags().BoolVar(&opts.detach, "detach", false, "Exit immediately instead of waiting for services to converge")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress progress output")
	cmd.Flags().BoolVar(&opts.noBuild, "no-build", false, "Skip building images before deploy")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Do not use cache when building")
	cmd.Flags().BoolVar(&opts.pull, "pull", false, "Always pull newer versions of base images")
	return cmd
}

type deployOptions struct {
	stack        string
	prune        bool
	resolveImage string
	detach       bool
	quiet        bool
	noBuild      bool
	noCache      bool
	pull         bool
}

func runDeployCommand(cmd *cobra.Command, args []string, cmdOpts *deployCommandOptions) error {
	opts := deployOptions{
		stack:        args[0],
		prune:        cmdOpts.prune,
		resolveImage: cmdOpts.resolveImage,
		detach:       cmdOpts.detach,
		quiet:        cmdOpts.quiet,
		noBuild:      cmdOpts.noBuild,
		noCache:      cmdOpts.noCache,
		pull:         cmdOpts.pull,
	}

	return runDeploy(cmd.Context(), opts, cmdOpts.composeFiles)
}

func runDeploy(ctx context.Context, opts deployOptions, files []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	cfg, err := loadConfig(cwd)
	if err != nil {
		return err
	}

	project, err := LoadCompose(ctx, os.Environ(), files...)
	if err != nil {
		return err
	}

	cicdezSecrets, err := loadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if err := processLocalConfigs(&project, cwd); err != nil {
		return fmt.Errorf("failed to process local_configs: %w", err)
	}

	if err := processSensitiveSecrets(&project, cicdezSecrets); err != nil {
		return fmt.Errorf("failed to process sensitive secrets: %w", err)
	}

	// Build and push images if not skipped
	if !opts.noBuild && hasBuildConfig(project) {
		dockerClient, err := client.New(client.WithHostFromEnv())
		if err != nil {
			return fmt.Errorf("failed to create local docker client: %w", err)
		}
		defer dockerClient.Close()

		buildOpts := BuildOptions{
			cwd:        cwd,
			registries: cfg.Registries,
			noCache:    opts.noCache,
			pull:       opts.pull,
			push:       true,
		}

		if err := Build(ctx, dockerClient, project, buildOpts); err != nil {
			return fmt.Errorf("failed to build and push images: %w", err)
		}
	}

	for _, server := range cfg.Servers {
		dockerClient, err := NewDockerClientSSH(server.Host, server.User, []byte(server.Key))
		if err != nil {
			return err
		}

		err = Deploy(ctx, dockerClient, project, DeployOptions{
			stack:        opts.stack,
			prune:        opts.prune,
			resolveImage: opts.resolveImage,
			detach:       opts.detach,
			quiet:        opts.quiet,
			registries:   cfg.Registries,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func hasBuildConfig(project types.Project) bool {
	for _, svc := range project.Services {
		if svc.Build != nil {
			return true
		}
	}
	return false
}

func processSensitiveSecrets(project *types.Project, allSecrets Secrets) error {
	if project.Secrets == nil {
		project.Secrets = make(types.Secrets)
	}

	for svcName, svc := range project.Services {
		for name, sensitive := range svc.Sensitive {
			content, err := formatSecretsForSensitive(allSecrets, sensitive.Secrets, sensitive.Format)
			if err != nil {
				return fmt.Errorf("failed to format sensitive secrets for service %s target %s: %w", svc.Name, sensitive.Target, err)
			}

			hash := sha256.Sum256(content)
			hashStr := hex.EncodeToString(hash[:])[:8]

			secretName := fmt.Sprintf("%s_%s", name, hashStr)

			project.Secrets[secretName] = types.SecretConfig{
				Content: string(content),
			}

			svc.Secrets = append(svc.Secrets, types.ServiceSecretConfig{
				Source: secretName,
				Target: sensitive.Target,
				UID:    sensitive.UID,
				GID:    sensitive.GID,
				Mode:   sensitive.Mode,
			})
		}
		project.Services[svcName] = svc
	}

	return nil
}
