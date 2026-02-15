package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

type RegistryClient interface {
	RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
}

var newRegistryClient = func() (RegistryClient, error) {
	return client.New(client.WithHostFromEnv())
}

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage Docker registry credentials",
	Long:  "Add, list, and remove Docker registry authentication credentials",
}

var registryAddCmd = &cobra.Command{
	Use:   "add <server>",
	Short: "Add or update a registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryAdd,
}

var registryListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all registries",
	RunE:    runRegistryList,
}

var registryRemoveCmd = &cobra.Command{
	Use:     "remove <server>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a registry",
	Args:    cobra.ExactArgs(1),
	RunE:    runRegistryRemove,
}

var (
	registryUsername string
	registryPassword string
)

func init() {
	registryAddCmd.Flags().StringVar(&registryUsername, "username", "", "Registry username (required)")
	registryAddCmd.Flags().StringVar(&registryPassword, "password", "", "Registry password (required)")
	registryAddCmd.MarkFlagRequired("username")
	registryAddCmd.MarkFlagRequired("password")

	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryRemoveCmd)
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	server := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	authConfig := registry.AuthConfig{
		Username:      registryUsername,
		Password:      registryPassword,
		ServerAddress: server,
	}
	dockerClient, err := newRegistryClient()
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	loginOpts := client.RegistryLoginOptions{
		Username:      registryUsername,
		Password:      registryPassword,
		ServerAddress: server,
	}
	resp, err := dockerClient.RegistryLogin(cmd.Context(), loginOpts)
	if err != nil {
		return err
	}
	if resp.Auth.IdentityToken != "" {
		authConfig.Password = ""
		authConfig.IdentityToken = resp.Auth.IdentityToken
	}

	config, err := loadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.Registries == nil {
		config.Registries = make(map[string]registry.AuthConfig)
	}

	config.Registries[server] = authConfig

	if err := saveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if resp.Auth.Status != "" {
		fmt.Println(resp.Auth.Status)
	}

	return nil
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := loadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Registries) == 0 {
		fmt.Println("No registries found")
		return nil
	}

	names := make([]string, 0, len(config.Registries))
	for name := range config.Registries {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Registries:")
	for _, name := range names {
		registry := config.Registries[name]
		fmt.Printf("  %s:\n", name)
		fmt.Printf("    URL: %s\n", registry.ServerAddress)
		fmt.Printf("    Username: %s\n", registry.Username)
		fmt.Printf("    Password: <configured>\n")
	}

	return nil
}

func runRegistryRemove(cmd *cobra.Command, args []string) error {
	server := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := loadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Registries[server]; !exists {
		return fmt.Errorf("registry '%s' not found", server)
	}

	delete(config.Registries, server)

	if err := saveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Registry '%s' removed\n", server)
	return nil
}
