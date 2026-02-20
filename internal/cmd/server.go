package cmd

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/blindlobstar/cicdez/internal/vault"
)

type serverAddOptions struct {
	name    string
	host    string
	port    int
	user    string
	keyFile string
}

type serverRemoveOptions struct {
	name string
}

type serverSetDefaultOptions struct {
	name string
}

func NewServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage deployment servers",
	}

	addOpts := &serverAddOptions{}
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.name = args[0]
			return runServerAdd(addOpts)
		},
	}
	addCmd.Flags().StringVarP(&addOpts.host, "host", "H", "", "Server hostname or IP address, optionally with port (host:port)")
	addCmd.Flags().StringVarP(&addOpts.user, "user", "u", "root", "SSH user")
	addCmd.Flags().StringVarP(&addOpts.keyFile, "key-file", "i", "", "Path to SSH private key file")
	addCmd.MarkFlagRequired("host")

	removeOpts := &serverRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.name = args[0]
			return runServerRemove(removeOpts)
		},
	}

	setDefaultOpts := &serverSetDefaultOptions{}
	setDefaultCmd := &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set server as default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setDefaultOpts.name = args[0]
			return runServerSetDefault(setDefaultOpts)
		},
	}

	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerList()
		},
	})
	cmd.AddCommand(removeCmd)
	cmd.AddCommand(setDefaultCmd)

	return cmd
}

func runServerAdd(opts *serverAddOptions) error {
	host := opts.host
	port := 22

	if h, p, err := net.SplitHostPort(opts.host); err == nil {
		host = h
		if pn, err := strconv.Atoi(p); err == nil {
			port = pn
		}
	}

	var keyContent string
	if opts.keyFile != "" {
		data, err := os.ReadFile(opts.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		keyContent = string(data)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	config.AddServer(opts.name, vault.Server{
		Host: host,
		Port: port,
		User: opts.user,
		Key:  keyContent,
	})

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Server '%s' added\n", opts.name)
	return nil
}

func runServerList() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Servers) == 0 {
		fmt.Println("No servers found")
		return nil
	}

	names := make([]string, 0, len(config.Servers))
	for name := range config.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Servers:")
	for _, name := range names {
		server := config.Servers[name]
		defaultMark := ""
		if name == config.DefaultServer {
			defaultMark = " *"
		}
		fmt.Printf("  %s%s:\n", name, defaultMark)
		port := server.Port
		if port == 0 {
			port = 22
		}
		fmt.Printf("    Host: %s:%d\n", server.Host, port)
		fmt.Printf("    User: %s\n", server.User)
		if server.Key != "" {
			fmt.Printf("    Key: <configured>\n")
		}
	}

	return nil
}

func runServerRemove(opts *serverRemoveOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if _, exists := config.Servers[opts.name]; !exists {
		return fmt.Errorf("server '%s' not found", opts.name)
	}

	newDefault := config.RemoveServer(opts.name)

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if newDefault != "" {
		fmt.Printf("Server '%s' removed. New default: %s\n", opts.name, newDefault)
	} else {
		fmt.Printf("Server '%s' removed\n", opts.name)
	}
	return nil
}

func runServerSetDefault(opts *serverSetDefaultOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := config.SetDefault(opts.name); err != nil {
		return err
	}

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Server '%s' set as default\n", opts.name)
	return nil
}
