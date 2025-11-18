package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage Docker registry credentials",
	Long:  "Add, list, and remove Docker registry authentication credentials",
}

var registryAddCmd = &cobra.Command{
	Use:   "add <name>",
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
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a registry",
	Args:    cobra.ExactArgs(1),
	RunE:    runRegistryRemove,
}

var (
	registryURL      string
	registryUsername string
	registryPassword string
)

func init() {
	registryAddCmd.Flags().StringVar(&registryURL, "url", "", "Registry URL (required)")
	registryAddCmd.Flags().StringVar(&registryUsername, "username", "", "Registry username (required)")
	registryAddCmd.Flags().StringVar(&registryPassword, "password", "", "Registry password (required)")
	registryAddCmd.MarkFlagRequired("url")
	registryAddCmd.MarkFlagRequired("username")
	registryAddCmd.MarkFlagRequired("password")

	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryRemoveCmd)
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		return fmt.Errorf("failed to load identity: %w", err)
	}

	recipients, err := loadRecipients(cwd)
	if err != nil {
		return fmt.Errorf("failed to load recipients: %w", err)
	}

	config, err := loadConfig(cwd, identity)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.Registries == nil {
		config.Registries = make(map[string]Registry)
	}

	config.Registries[name] = Registry{
		URL:      registryURL,
		Username: registryUsername,
		Password: registryPassword,
	}

	if err := saveConfig(cwd, recipients, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Registry '%s' added\n", name)
	return nil
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		return fmt.Errorf("failed to load identity: %w", err)
	}

	config, err := loadConfig(cwd, identity)
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
		fmt.Printf("    URL: %s\n", registry.URL)
		fmt.Printf("    Username: %s\n", registry.Username)
		fmt.Printf("    Password: <configured>\n")
	}

	return nil
}

func runRegistryRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	identity, err := loadIdentity()
	if err != nil {
		return fmt.Errorf("failed to load identity: %w", err)
	}

	recipients, err := loadRecipients(cwd)
	if err != nil {
		return fmt.Errorf("failed to load recipients: %w", err)
	}

	config, err := loadConfig(cwd, identity)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Registries[name]; !exists {
		return fmt.Errorf("registry '%s' not found", name)
	}

	delete(config.Registries, name)

	if err := saveConfig(cwd, recipients, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Registry '%s' removed\n", name)
	return nil
}
