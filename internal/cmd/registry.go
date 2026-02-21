package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/spf13/cobra"
)

type RegistryClient interface {
	RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
}

type RegistryClientFactory func() (RegistryClient, error)

func DefaultRegistryClientFactory() (RegistryClient, error) {
	return client.New(client.WithHostFromEnv())
}

type registryAddOptions struct {
	server        string
	username      string
	password      string
	clientFactory RegistryClientFactory
}

type registryRemoveOptions struct {
	server string
}

func NewRegistryCommand() *cobra.Command {
	return NewRegistryCommandWithFactory(DefaultRegistryClientFactory)
}

func NewRegistryCommandWithFactory(clientFactory RegistryClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage Docker registry credentials",
	}

	addOpts := registryAddOptions{clientFactory: clientFactory}
	addCmd := &cobra.Command{
		Use:   "add <server>",
		Short: "Add or update a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.server = args[0]
			return runRegistryAdd(cmd.Context(), cmd.OutOrStdout(), addOpts)
		},
	}
	addCmd.Flags().StringVar(&addOpts.username, "username", "", "Registry username (required)")
	addCmd.Flags().StringVar(&addOpts.password, "password", "", "Registry password (required)")
	addCmd.MarkFlagRequired("username")
	addCmd.MarkFlagRequired("password")

	removeOpts := registryRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove <server>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a registry",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.server = args[0]
			return runRegistryRemove(cmd.OutOrStdout(), removeOpts)
		},
	}

	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all registries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegistryList(cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(removeCmd)

	return cmd
}

func runRegistryAdd(ctx context.Context, out io.Writer, opts registryAddOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	authConfig := registry.AuthConfig{
		Username:      opts.username,
		Password:      opts.password,
		ServerAddress: opts.server,
	}

	dockerClient, err := opts.clientFactory()
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}

	loginOpts := client.RegistryLoginOptions{
		Username:      opts.username,
		Password:      opts.password,
		ServerAddress: opts.server,
	}

	resp, err := dockerClient.RegistryLogin(ctx, loginOpts)
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

	config.Registries[opts.server] = authConfig

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if resp.Auth.Status != "" {
		fmt.Fprintln(out, resp.Auth.Status)
	}

	return nil
}

func runRegistryList(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Registries) == 0 {
		fmt.Fprintln(out, "No registries found")
		return nil
	}

	names := make([]string, 0, len(config.Registries))
	for name := range config.Registries {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Registries:")
	for _, name := range names {
		reg := config.Registries[name]
		fmt.Fprintf(out, "  %s:\n", name)
		fmt.Fprintf(out, "    URL: %s\n", reg.ServerAddress)
		fmt.Fprintf(out, "    Username: %s\n", reg.Username)
		fmt.Fprintln(out, "    Password: <configured>")
	}

	return nil
}

func runRegistryRemove(out io.Writer, opts registryRemoveOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Registries[opts.server]; !exists {
		return fmt.Errorf("registry '%s' not found", opts.server)
	}

	delete(config.Registries, opts.server)

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(out, "Registry '%s' removed\n", opts.server)
	return nil
}
