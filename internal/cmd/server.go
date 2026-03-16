package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blindlobstar/cicdez/internal/docker"
	"github.com/blindlobstar/cicdez/internal/ssh"
	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	dockerclient "github.com/moby/moby/client"
	"github.com/spf13/cobra"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type serverAddOptions struct {
	host    string
	user    string
	keyFile string
}

type serverRemoveOptions struct {
	name string
}

type serverInitOptions struct {
	host                string
	port                int
	user                string
	rootKey             string
	disablePasswordAuth bool
	deployerUser        string
	worker              bool
	dryRun              bool
}

func NewServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage deployment servers",
	}

	addOpts := serverAddOptions{}
	addCmd := &cobra.Command{
		Use:   "add HOST",
		Short: "Add or update a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.host = args[0]
			return runServerAdd(cmd.Context(), cmd.OutOrStdout(), addOpts)
		},
	}
	addCmd.Flags().StringVarP(&addOpts.user, "user", "u", "root", "ssh user")
	addCmd.Flags().StringVarP(&addOpts.keyFile, "key-file", "i", "", "path to ssh private key file")
	addCmd.MarkFlagRequired("host")

	removeOpts := serverRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove HOST",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.name = args[0]
			return runServerRemove(cmd.Context(), cmd.OutOrStdout(), removeOpts)
		},
	}

	initOpts := serverInitOptions{port: 22, user: "root", deployerUser: "deployer"}
	initCmd := &cobra.Command{
		Use:   "init HOST",
		Short: "Initialize a fresh server for Docker Swarm deployments",
		Long: `Provision a fresh server for Docker Swarm deployments.

This command connects to a server, creates a deployer user, installs Docker,
initializes a Docker Swarm, and saves the configuration.

Example:
  cicdez server init 192.168.1.100
  cicdez server init example.com -i ~/.ssh/id_ed25519`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			initOpts.host = args[1]
			in, _ := cmd.InOrStdin().(*os.File)
			if in == nil {
				in = os.Stdin
			}
			return runServerInit(cmd.Context(), in, cmd.OutOrStdout(), initOpts)
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

	return cmd
}

func runServerAdd(ctx context.Context, out io.Writer, opts serverAddOptions) error {
	port := 22

	if h, p, err := net.SplitHostPort(opts.host); err == nil {
		opts.host = h
		if pn, err := strconv.Atoi(p); err == nil {
			port = pn
		}
	}

	var key []byte
	if opts.keyFile != "" {
		data, err := os.ReadFile(opts.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		key = data
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	node, err := docker.NewClientSSH(opts.host, port, opts.user, key)
	if err != nil {
		return err
	}

	info, err := node.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}
	if info.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		// TODO: error message: use cicdez init
		return nil
	}

	manager, err := docker.GetManagerClient(ctx, config.Servers)
	if err != nil && !errors.Is(err, docker.ErrManagerNotFound) {
		return err
	}

	// if node is a worker, we should check if it's part of a cluster
	if !info.Info.Swarm.ControlAvailable {
		addresses := make([]string, 0, len(info.Info.Swarm.RemoteManagers))
		for _, p := range info.Info.Swarm.RemoteManagers {
			addresses = append(addresses, p.Addr)
		}
		if !slices.Contains(addresses, manager.DaemonHost()) {
			// TODO: error message
			return nil
		}
	}

	inspect, err := manager.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
	if err != nil {
		return err
	}

	token := inspect.Swarm.JoinTokens.Manager
	if info.Info.Swarm.ControlAvailable {
		token = inspect.Swarm.JoinTokens.Worker
	}

	if _, err := node.SwarmJoin(ctx, dockerclient.SwarmJoinOptions{
		AdvertiseAddr: opts.host,
		RemoteAddrs:   []string{manager.DaemonHost()},
		JoinToken:     token,
	}); err != nil {
		return err
	}

	config.Servers[opts.host] = vault.Server{
		Port: port,
		User: opts.user,
		Key:  string(key),
	}

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(out, "Server '%s' added\n", opts.host)
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

	hosts := make([]string, 0, len(config.Servers))
	for host := range config.Servers {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	fmt.Fprintln(out, "Servers:")
	for _, host := range hosts {
		server := config.Servers[host]

		port := server.Port
		if port == 0 {
			port = 22
		}
		fmt.Fprintf(out, "\tHost: %s:%d\n", host, port)
		fmt.Fprintf(out, "\tUser: %s\n", server.User)
		if server.Key != "" {
			fmt.Fprintln(out, "\tKey: <configured>")
		}
	}

	return nil
}

func runServerRemove(ctx context.Context, out io.Writer, opts serverRemoveOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	server, exists := config.Servers[opts.name]
	if !exists {
		return fmt.Errorf("server '%s' not found", opts.name)
	}

	node, err := docker.NewClientSSH(opts.name, server.Port, server.User, []byte(server.Key))
	if err != nil {
		return err
	}

	info, err := node.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}
	nodeID := info.Info.Swarm.NodeID
	worker := !info.Info.Swarm.ControlAvailable

	// TODO: trying to remove only manager node
	manager, err := docker.GetManagerClient(ctx, config.Servers, opts.name)
	if err != nil {
		return err
	}

	ni, err := manager.NodeInspect(ctx, nodeID, dockerclient.NodeInspectOptions{})
	if err != nil {
		return err
	}
	if !worker {
		ni.Node.Spec.Role = swarm.NodeRoleWorker
	}
	ni.Node.Spec.Availability = swarm.NodeAvailabilityDrain

	_, err = manager.NodeUpdate(ctx, nodeID, dockerclient.NodeUpdateOptions{
		Version: ni.Node.Version,
		Spec:    ni.Node.Spec,
	})
	if err != nil {
		return nil
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	filters := make(client.Filters)
	filters.Add("node", nodeID)
	filters.Add("desired-state", "running")

wait:
	for {
		select {
		case <-cctx.Done():
			return cctx.Err()
		case <-time.Tick(2 * time.Second):
			tasks, err := node.TaskList(cctx, dockerclient.TaskListOptions{
				Filters: filters,
			})
			if err != nil {
				return err
			}

			if len(tasks.Items) == 0 {
				break wait
			}
		}
	}

	_, err = node.SwarmLeave(ctx, dockerclient.SwarmLeaveOptions{
		Force: false,
	})
	if err != nil {
		return err
	}

	_, err = manager.NodeRemove(ctx, nodeID, dockerclient.NodeRemoveOptions{
		Force: false,
	})
	if err != nil {
		return err
	}

	delete(config.Servers, opts.name)

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(out, "Server '%s' removed\n", opts.name)
	return nil
}

func runServerInit(ctx context.Context, in *os.File, out io.Writer, opts serverInitOptions) error {
	sudo := opts.user != "root"

	homeDir, _ := os.UserHomeDir()
	rootKeyPath := filepath.Join(homeDir, ".ssh", opts.host+"-"+opts.user)

	if opts.rootKey == "" {
		if _, err := os.Stat(rootKeyPath); err == nil {
			opts.rootKey = rootKeyPath
		}
	}

	var client *gossh.Client
	var err error

	fmt.Fprintf(out, "Connecting to %s@%s:%d...\n", opts.user, opts.host, opts.port)
	if opts.rootKey != "" {
		keyData, err := os.ReadFile(opts.rootKey)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		client, err = ssh.DialWithKey(opts.host, opts.port, opts.user, keyData)
	} else {
		fmt.Fprintf(out, "Enter password for %s: ", opts.user)

		passwordBytes, err := term.ReadPassword(int(in.Fd()))
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		password := string(passwordBytes)

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

	stdout, _, err := ssh.Run(client, "docker --version", sudo)
	if err != nil {
		return err
	}
	if stdout == "" {
		fmt.Fprintln(out, "Installing Docker...")
		if !opts.dryRun {
			_, stderr, err := ssh.Run(client, "curl -fsSL https://get.docker.com | sh && systemctl enable docker.service && systemctl enable containerd.service", sudo)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
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

	server := config.Servers[opts.host]
	server.Port = opts.port
	if server.User != opts.deployerUser {
		server.Key = ""
	}
	server.User = opts.deployerUser

	_, stderr, err := ssh.Run(client, fmt.Sprintf("id %s", server.User), false)
	if err != nil {
		return err
	}
	if stderr != "" {
		fmt.Fprintf(out, "Creating user '%s'...\n", server.User)
		if !opts.dryRun {
			_, stderr, err := ssh.Run(client, fmt.Sprintf("adduser --disabled-password --comment '' %s", server.User), sudo)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
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
		_, stderr, err := ssh.Run(client, fmt.Sprintf("usermod -aG docker %s", opts.deployerUser), sudo)
		if err != nil || stderr != "" {
			return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
		}
	}

	if opts.disablePasswordAuth {
		fmt.Fprintln(out, "Disabling password authentication...")
		if !opts.dryRun {
			_, stderr, err := ssh.Run(client, "sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && systemctl restart sshd", sudo)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
			}
		}
	}

	fmt.Fprintln(out, "Saving configuration...")
	if !opts.dryRun {
		config.Servers[opts.host] = server

		if err := vault.SaveConfig(cwd, config); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	dockerClient, err := docker.NewClientSSH(opts.host, server.Port, server.User, []byte(server.Key))
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	info, err := dockerClient.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}

	if info.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		fmt.Fprintln(out, "Initializing Docker Swarm...")
		if !opts.dryRun {
			dockerClient.SwarmInit(ctx, dockerclient.SwarmInitOptions{
				AdvertiseAddr: client.RemoteAddr().String(),
			})
			if err != nil {
				return err
			}
		}
	}

	if err := joinSwarm(ctx, dockerClient, opts.host, config.Servers, opts.worker, opts.dryRun); err != nil {
		return err
	}

	if opts.dryRun {
		fmt.Fprintln(out, "\nDry run complete. No changes were made.")
	} else {
		fmt.Fprintf(out, "\nServer '%s' initialized successfully.\n", opts.host)
	}

	return nil
}

func joinSwarm(ctx context.Context, node client.APIClient, host string, servers map[string]vault.Server, worker, dryRun bool) error {
	manager, err := docker.GetManagerClient(ctx, servers, host)
	if errors.Is(err, docker.ErrManagerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer manager.Close()

	inspect, err := manager.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
	if err != nil {
		return err
	}

	token := inspect.Swarm.JoinTokens.Manager
	if worker {
		token = inspect.Swarm.JoinTokens.Worker
	}

	if dryRun {
		if _, err := node.SwarmJoin(ctx, dockerclient.SwarmJoinOptions{
			AdvertiseAddr: host,
			RemoteAddrs:   []string{manager.DaemonHost()},
			JoinToken:     token,
		}); err != nil {
			return err
		}
	}
	return nil
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
	_, stderr, err := ssh.Run(client, script, sudo)
	if err != nil || stderr != "" {
		return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
	}
	return err
}

func keyInstalled(client *gossh.Client, user, pubKey string) bool {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	stdout, stderr, err := ssh.Run(client, fmt.Sprintf("cat %s/.ssh/authorized_keys 2>/dev/null", home), false)
	if err != nil || stderr != "" {
		return false
	}
	return strings.Contains(stdout, pubKey)
}
