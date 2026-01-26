package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage deployment servers",
	Long:  "Add, list, and remove servers configured for deployment",
}

var serverAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add or update a server",
	Args:  cobra.ExactArgs(1),
	RunE:  runServerAdd,
}

var serverListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all servers",
	RunE:    runServerList,
}

var serverRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a server",
	Args:    cobra.ExactArgs(1),
	RunE:    runServerRemove,
}

var (
	serverHost string
	serverUser string
	serverKey  string
)

func init() {
	serverAddCmd.Flags().StringVar(&serverHost, "host", "", "Server hostname or IP address (required)")
	serverAddCmd.Flags().StringVar(&serverUser, "user", "", "SSH user (required)")
	serverAddCmd.Flags().StringVar(&serverKey, "key", "", "SSH private key (optional)")
	serverAddCmd.MarkFlagRequired("host")
	serverAddCmd.MarkFlagRequired("user")

	serverCmd.AddCommand(serverAddCmd)
	serverCmd.AddCommand(serverListCmd)
	serverCmd.AddCommand(serverRemoveCmd)
}

func runServerAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	config, err := loadConfig(e, cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.Servers == nil {
		config.Servers = make(map[string]Server)
	}

	config.Servers[name] = Server{
		Host: serverHost,
		User: serverUser,
		Key:  serverKey,
	}

	if err := saveConfig(e, cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Server '%s' added\n", name)
	return nil
}

func runServerList(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	config, err := loadConfig(e, cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Servers) == 0 {
		fmt.Println("No servers found")
		return nil
	}

	// Sort server names for consistent output
	names := make([]string, 0, len(config.Servers))
	for name := range config.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Servers:")
	for _, name := range names {
		server := config.Servers[name]
		fmt.Printf("  %s:\n", name)
		fmt.Printf("    Host: %s\n", server.Host)
		fmt.Printf("    User: %s\n", server.User)
		if server.Key != "" {
			fmt.Printf("    Key: <configured>\n")
		}
	}

	return nil
}

func runServerRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	config, err := loadConfig(e, cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Servers[name]; !exists {
		return fmt.Errorf("server '%s' not found", name)
	}

	delete(config.Servers, name)

	if err := saveConfig(e, cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Server '%s' removed\n", name)
	return nil
}
