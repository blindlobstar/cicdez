package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy [stack]",
	Short: "Deploy a stack to Docker Swarm",
	Long:  "Deploy services defined in compose file to Docker Swarm cluster",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeployCommand,
}

var (
	deployComposeFiles []string
	deployPrune        bool
	deployResolveImage string
	deployDetach       bool
	deployQuiet        bool
)

func init() {
	deployCmd.Flags().StringArrayVarP(&deployComposeFiles, "file", "f", []string{"compose.yaml"}, "Compose file path(s)")
	deployCmd.Flags().BoolVar(&deployPrune, "prune", false, "Prune services that are no longer referenced")
	deployCmd.Flags().StringVar(&deployResolveImage, "resolve-image", resolveImageAlways, "Query the registry to resolve image digest and supported platforms (\"always\", \"changed\", \"never\")")
	deployCmd.Flags().BoolVar(&deployDetach, "detach", false, "Exit immediately instead of waiting for services to converge")
	deployCmd.Flags().BoolVarP(&deployQuiet, "quiet", "q", false, "Suppress progress output")
}

type deployOptions struct {
	stack        string
	prune        bool
	resolveImage string
	detach       bool
	quiet        bool
}

func runDeployCommand(cmd *cobra.Command, args []string) error {
	opts := deployOptions{
		stack:        args[0],
		prune:        deployPrune,
		resolveImage: deployResolveImage,
		detach:       deployDetach,
		quiet:        deployQuiet,
	}

	return runDeploy(cmd.Context(), opts, deployComposeFiles)
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
