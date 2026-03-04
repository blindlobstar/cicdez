package cmd

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/blindlobstar/cicdez/internal/ssh"
	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/spf13/cobra"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type serverAddOptions struct {
	name    string
	host    string
	user    string
	keyFile string
}

type serverRemoveOptions struct {
	name string
}

type serverSetDefaultOptions struct {
	name string
}

type serverInitOptions struct {
	name                string
	host                string
	port                int
	user                string
	rootKey             string
	disablePasswordAuth bool
	deployerUser        string
	dryRun              bool
}

func NewServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage deployment servers",
	}

	addOpts := serverAddOptions{}
	addCmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add or update a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.name = args[0]
			return runServerAdd(cmd.OutOrStdout(), addOpts)
		},
	}
	addCmd.Flags().StringVarP(&addOpts.host, "host", "H", "", "hostname or IP with optional port (HOST:PORT)")
	addCmd.Flags().StringVarP(&addOpts.user, "user", "u", "root", "ssh user")
	addCmd.Flags().StringVarP(&addOpts.keyFile, "key-file", "i", "", "path to ssh private key file")
	addCmd.MarkFlagRequired("host")

	removeOpts := serverRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.name = args[0]
			return runServerRemove(cmd.OutOrStdout(), removeOpts)
		},
	}

	setDefaultOpts := serverSetDefaultOptions{}
	setDefaultCmd := &cobra.Command{
		Use:   "set-default NAME",
		Short: "Set default server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setDefaultOpts.name = args[0]
			return runServerSetDefault(cmd.OutOrStdout(), setDefaultOpts)
		},
	}

	initOpts := serverInitOptions{port: 22, user: "root", deployerUser: "deployer"}
	initCmd := &cobra.Command{
		Use:   "init NAME HOST",
		Short: "Initialize a fresh server for Docker Swarm deployments",
		Long: `Provision a fresh server for Docker Swarm deployments.

This command connects to a server, creates a deployer user, installs Docker,
initializes a Docker Swarm, and saves the configuration.

Example:
  cicdez server init production 192.168.1.100
  cicdez server init staging example.com -i ~/.ssh/id_ed25519`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			initOpts.name = args[0]
			initOpts.host = args[1]
			in, _ := cmd.InOrStdin().(*os.File)
			if in == nil {
				in = os.Stdin
			}
			return runServerInit(in, cmd.OutOrStdout(), initOpts)
		},
	}
	initCmd.Flags().StringVarP(&initOpts.user, "user", "u", "root", "SSH user for initial connection")
	initCmd.Flags().StringVarP(&initOpts.rootKey, "root-key", "i", "", "path to SSH private key")
	initCmd.Flags().BoolVar(&initOpts.disablePasswordAuth, "disable-password-auth", false, "disable SSH password auth after setup")
	initCmd.Flags().StringVar(&initOpts.deployerUser, "deployer-user", "deployer", "deployer username")
	initCmd.Flags().IntVarP(&initOpts.port, "port", "p", 22, "SSH port")
	initCmd.Flags().BoolVar(&initOpts.dryRun, "dry-run", false, "print what would be done without making changes")

	cmd.AddCommand(initCmd)
	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerList(cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(removeCmd)
	cmd.AddCommand(setDefaultCmd)

	return cmd
}

func runServerAdd(out io.Writer, opts serverAddOptions) error {
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

	fmt.Fprintf(out, "Server '%s' added\n", opts.name)
	return nil
}

func runServerList(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(config.Servers) == 0 {
		fmt.Fprintln(out, "No servers found")
		return nil
	}

	names := make([]string, 0, len(config.Servers))
	for name := range config.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Servers:")
	for _, name := range names {
		server := config.Servers[name]
		defaultMark := ""
		if name == config.DefaultServer {
			defaultMark = " *"
		}
		fmt.Fprintf(out, "  %s%s:\n", name, defaultMark)
		port := server.Port
		if port == 0 {
			port = 22
		}
		fmt.Fprintf(out, "\tHost: %s:%d\n", server.Host, port)
		fmt.Fprintf(out, "\tUser: %s\n", server.User)
		if server.Key != "" {
			fmt.Fprintln(out, "\tKey: <configured>")
		}
	}

	return nil
}

func runServerRemove(out io.Writer, opts serverRemoveOptions) error {
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
		fmt.Fprintf(out, "Server '%s' removed. New default: %s\n", opts.name, newDefault)
	} else {
		fmt.Fprintf(out, "Server '%s' removed\n", opts.name)
	}
	return nil
}

func runServerSetDefault(out io.Writer, opts serverSetDefaultOptions) error {
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

	fmt.Fprintf(out, "Server '%s' set as default\n", opts.name)
	return nil
}

func runServerInit(in *os.File, out io.Writer, opts serverInitOptions) error {
	sudo := opts.user != "root"

	homeDir, _ := os.UserHomeDir()
	rootKeyPath := filepath.Join(homeDir, ".ssh", opts.name+"-"+opts.user)

	if opts.rootKey == "" {
		if _, err := os.Stat(rootKeyPath); err == nil {
			opts.rootKey = rootKeyPath
		}
	}

	var client *gossh.Client
	var err error

	fmt.Fprintf(out, "Connecting to %s@%s:%d...\n", opts.user, opts.host, opts.port)
	if opts.rootKey != "" {
		client, err = ssh.DialWithKey(opts.host, opts.port, opts.user, opts.rootKey)
	} else {
		fmt.Fprintf(out, "Enter password for %s: ", opts.user)
		var password string
		password, err = readPassword(in)
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		client, err = ssh.DialWithPassword(opts.host, opts.port, opts.user, password)

	}
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	if opts.rootKey == "" {
		fmt.Fprintln(out, "Generating SSH key...")
		if !opts.dryRun {
			privKey, pk, err := ssh.GenerateEd25519KeyPair()
			if err != nil {
				return fmt.Errorf("failed to generate key: %w", err)
			}

			fmt.Fprintf(out, "Saving key to %s...\n", rootKeyPath)
			if err := os.WriteFile(rootKeyPath, []byte(privKey), 0o600); err != nil {
				return fmt.Errorf("failed to save key: %w", err)
			}
			if err := ensureAuthorizedKey(client, sudo, opts.user, pk); err != nil {
				return fmt.Errorf("failed to install key: %w", err)
			}
		}
	}

	_, err = ssh.Run(client, "docker --version", sudo)
	if err != nil {
		fmt.Fprintln(out, "Installing Docker...")
		if !opts.dryRun {
			_, err := ssh.Run(client, "curl -fsSL https://get.docker.com | sh && systemctl enable docker.service && systemctl enable containerd.service", sudo)
			if err != nil {
				return fmt.Errorf("failed to install docker: %w", err)
			}
		}
	}

	stdout, err := ssh.Run(client, "docker info --format '{{.Swarm.LocalNodeState}}'", sudo)
	if err != nil || strings.TrimSpace(stdout) != "active" {
		fmt.Fprintln(out, "Initializing Docker Swarm...")
		if !opts.dryRun {
			stdout, err := ssh.Run(client, "hostname -I | awk '{print $1}'", false)
			if err != nil {
				return fmt.Errorf("failed to get IP address: %w", err)
			}
			ip := strings.TrimSpace(stdout)
			if ip == "" {
				return fmt.Errorf("could not determine server IP address")
			}
			_, err = ssh.Run(client, fmt.Sprintf("docker swarm init --advertise-addr %s", ip), sudo)
			if err != nil {
				return fmt.Errorf("failed to init swarm: %w", err)
			}
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	server := config.Servers[opts.name]
	server.Host = opts.host
	server.Port = opts.port
	if server.User != opts.deployerUser {
		server.Key = ""
	}
	server.User = opts.deployerUser

	_, err = ssh.Run(client, fmt.Sprintf("id %s", server.User), false)
	if err != nil {
		fmt.Fprintf(out, "Creating user '%s'...\n", server.User)
		if !opts.dryRun {
			_, err := ssh.Run(client, fmt.Sprintf("adduser --disabled-password --comment '' %s", server.User), sudo)
			if err != nil {
				return fmt.Errorf("failed to create deployer user: %w", err)
			}
		}
	}

	var publicKey string
	if server.Key == "" {
		private, public, err := ssh.GenerateEd25519KeyPair()
		if err != nil {
			return fmt.Errorf("failed to generate key: %w", err)
		}
		server.Key = private
		publicKey = public
	} else {
		signer, err := gossh.ParsePrivateKey([]byte(server.Key))
		if err != nil {
			return err
		}
		publicKey = strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))

		if keyInstalled(client, server.User, publicKey) {
			publicKey = ""
		}
	}

	if publicKey != "" {
		if !opts.dryRun {
			if err := ensureAuthorizedKey(client, sudo, server.User, publicKey); err != nil {
				return fmt.Errorf("failed to install key: %w", err)
			}
		}
	}

	if !opts.dryRun {
		_, err := ssh.Run(client, fmt.Sprintf("usermod -aG docker %s", opts.deployerUser), sudo)
		if err != nil {
			return err
		}
	}

	if opts.disablePasswordAuth {
		fmt.Fprintln(out, "Disabling password authentication...")
		if !opts.dryRun {
			_, err := ssh.Run(client, "sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && systemctl restart sshd", sudo)
			if err != nil {
				return fmt.Errorf("failed to disable password auth: %w", err)
			}
		}
	}

	fmt.Fprintln(out, "Saving configuration...")
	if !opts.dryRun {
		config.AddServer(opts.name, server)

		if err := vault.SaveConfig(cwd, config); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	if opts.dryRun {
		fmt.Fprintln(out, "\nDry run complete. No changes were made.")
	} else {
		fmt.Fprintf(out, "\nServer '%s' initialized successfully.\n", opts.name)
	}

	return nil
}

func readPassword(in *os.File) (string, error) {
	passwordBytes, err := term.ReadPassword(int(in.Fd()))
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

func ensureAuthorizedKey(client *gossh.Client, sudo bool, user, pubKey string) error {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	sshDir := home + "/.ssh"
	pubKey = strings.TrimSpace(pubKey)
	script := fmt.Sprintf(`mkdir -p %s && (grep -qF '%s' %s/authorized_keys 2>/dev/null || echo '%s' >> %s/authorized_keys) && chown -R %s:%s %s && chmod 700 %s && chmod 600 %s/authorized_keys`,
		sshDir, pubKey, sshDir, pubKey, sshDir, user, user, sshDir, sshDir, sshDir)
	_, err := ssh.Run(client, script, sudo)
	if err != nil {
		return err
	}
	return err
}

func keyInstalled(client *gossh.Client, user, pubKey string) bool {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	stdout, _ := ssh.Run(client, fmt.Sprintf("cat %s/.ssh/authorized_keys 2>/dev/null", home), false)
	return strings.Contains(stdout, pubKey)
}
