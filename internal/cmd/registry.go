package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
	"github.com/vrotherford/cicdez/internal/vault"
)

type RegistryClient interface {
	RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
}

type RegistryClientFactory func() (RegistryClient, error)

func DefaultRegistryClientFactory() (RegistryClient, error) {
	return client.New(client.WithHostFromEnv())
}

type registryAddOptions struct {
	username      string
	password      string
	clientFactory RegistryClientFactory
}

func NewRegistryCommand() *cobra.Command {
	return NewRegistryCommandWithFactory(DefaultRegistryClientFactory)
}

func NewRegistryCommandWithFactory(clientFactory RegistryClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage Docker registry credentials",
		Long:  "Add, list, and remove Docker registry authentication credentials",
	}
	cmd.AddCommand(newRegistryAddCommand(clientFactory))
	cmd.AddCommand(newRegistryListCommand())
	cmd.AddCommand(newRegistryRemoveCommand())
	return cmd
}

func newRegistryAddCommand(clientFactory RegistryClientFactory) *cobra.Command {
	opts := &registryAddOptions{
		clientFactory: clientFactory,
	}
	cmd := &cobra.Command{
		Use:   "add <server>",
		Short: "Add or update a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegistryAdd(cmd, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.username, "username", "", "Registry username (required)")
	cmd.Flags().StringVar(&opts.password, "password", "", "Registry password (required)")
	cmd.MarkFlagRequired("username")
	cmd.MarkFlagRequired("password")
	return cmd
}

func newRegistryListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all registries",
		RunE:    runRegistryList,
	}
}

func newRegistryRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <server>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a registry",
		Args:    cobra.ExactArgs(1),
		RunE:    runRegistryRemove,
	}
}

func runRegistryAdd(cmd *cobra.Command, args []string, opts *registryAddOptions) error {
	server := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	authConfig := registry.AuthConfig{
		Username:      opts.username,
		Password:      opts.password,
		ServerAddress: server,
	}
	dockerClient, err := opts.clientFactory()
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	loginOpts := client.RegistryLoginOptions{
		Username:      opts.username,
		Password:      opts.password,
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

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.Registries == nil {
		config.Registries = make(map[string]registry.AuthConfig)
	}

	config.Registries[server] = authConfig

	if err := vault.SaveConfig(cwd, config); err != nil {
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

	config, err := vault.LoadConfig(cwd)
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
		reg := config.Registries[name]
		fmt.Printf("  %s:\n", name)
		fmt.Printf("    URL: %s\n", reg.ServerAddress)
		fmt.Printf("    Username: %s\n", reg.Username)
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

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Registries[server]; !exists {
		return fmt.Errorf("registry '%s' not found", server)
	}

	delete(config.Registries, server)

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Registry '%s' removed\n", server)
	return nil
}
