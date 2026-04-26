package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/blindlobstar/cicdez/internal/docker"
	"github.com/blindlobstar/cicdez/internal/git"
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
	detach       bool
	ref          string
}

func NewDeployCommand() *cobra.Command {
	opts := deployOptions{}
	cmd := &cobra.Command{
		Use:   "deploy [STACK]",
		Short: "Deploy stack to Docker Swarm",
		Long: `Build images, push to registry, and deploy stack to Docker Swarm via SSH.

Images are built and pushed automatically unless --no-build is specified.
Secrets are decrypted and injected during deployment.
Stack name defaults to the project name from the compose file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.stack = args[0]
			}
			return runDeploy(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringArrayVarP(&opts.composeFiles, "file", "f", []string{}, "compose file path(s)")
	cmd.Flags().StringVar(&opts.ref, "ref", "", "git ref to deploy (branch, tag, or sha); bare --ref means HEAD")
	cmd.Flags().Lookup("ref").NoOptDefVal = "HEAD"
	cmd.Flags().BoolVar(&opts.prune, "prune", false, "prune services no longer referenced")
	cmd.Flags().StringVar(&opts.resolveImage, "resolve-image", docker.ResolveImageAlways, "resolve image digests: always, changed, never")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "suppress progress output")
	cmd.Flags().BoolVar(&opts.noBuild, "no-build", false, "skip building images before deploy")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "do not use cache when building")
	cmd.Flags().BoolVar(&opts.pull, "pull", false, "pull newer versions of base images")
	cmd.Flags().BoolVarP(&opts.detach, "detach", "d", false, "exit immediately instead of waiting for the services to converge")
	return cmd
}

func runDeploy(ctx context.Context, out io.Writer, opts deployOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	if opts.ref != "" {
		dir, cleanup, err := git.Resolve(cwd, opts.ref)
		if err != nil {
			return err
		}
		defer cleanup()
		cwd = dir
	}

	cfg, err := vault.LoadConfig(cwd)
	if err != nil {
		return err
	}

	project, err := docker.LoadCompose(ctx, cwd, opts.composeFiles...)
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
			Registries: cfg.Registries,
			NoCache:    opts.noCache,
			Pull:       opts.pull,
			Push:       true,
			Out:        out,
		}

		if !opts.quiet {
			fmt.Fprintln(out, "==> Building images")
		}
		if err := docker.Build(ctx, dockerClient, project, buildOpts); err != nil {
			return fmt.Errorf("failed to build and push images: %w", err)
		}
		if !opts.quiet {
			fmt.Fprintln(out)
		}
	}

	client, _, err := docker.GetManagerClient(ctx, cfg.Servers)
	if err != nil {
		return err
	}
	defer client.Close()

	if !opts.quiet {
		fmt.Fprintf(out, "==> Deploying stack %s\n", opts.stack)
	}
	err = docker.Deploy(ctx, client, project, docker.DeployOptions{
		Secrets:      secrets,
		Stack:        opts.stack,
		Prune:        opts.prune,
		ResolveImage: opts.resolveImage,
		Quiet:        opts.quiet,
		Registries:   cfg.Registries,
		Detach:       opts.detach,
		Out:          out,
	})
	if err != nil {
		return err
	}

	return nil
}
