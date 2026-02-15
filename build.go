package main

import (
	"fmt"
	"os"

	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build [services...]",
	Short: "Build images from compose file",
	Long:  "Build Docker images for services defined in compose file",
	RunE:  runBuild,
}

var (
	buildComposeFile string
	buildNoCache     bool
	buildPull        bool
	buildPush        bool
)

func init() {
	buildCmd.Flags().StringVarP(&buildComposeFile, "file", "f", "compose.yaml", "Compose file path")
	buildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "Do not use cache when building")
	buildCmd.Flags().BoolVar(&buildPull, "pull", false, "Always pull newer versions of base images")
	buildCmd.Flags().BoolVar(&buildPush, "push", false, "Push images after build")
}

func runBuild(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	project, err := LoadCompose(ctx, nil, buildComposeFile)
	if err != nil {
		return fmt.Errorf("failed to load compose file: %w", err)
	}

	config, err := loadConfig(cwd)
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

	opts := BuildOptions{
		services:   servicesToBuild,
		cwd:        cwd,
		registries: config.Registries,
		noCache:    buildNoCache,
		pull:       buildPull,
		push:       buildPush,
	}

	return Build(ctx, dockerClient, project, opts)
}
