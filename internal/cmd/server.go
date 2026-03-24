package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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

func NewServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage deployment servers",
	}

	cmd.AddCommand(newServerAddCommand())
	cmd.AddCommand(newServerListCommand())
	cmd.AddCommand(newServerRemoveCommand())
	cmd.AddCommand(newServerInitCommand())

	return cmd
}

func newServerAddCommand() *cobra.Command {
	opts := serverAddOptions{}
	cmd := &cobra.Command{
		Use:   "add HOST",
		Short: "Add or update a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.host = args[0]
			return runServerAdd(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVarP(&opts.user, "user", "u", "root", "ssh user")
	cmd.Flags().StringVarP(&opts.keyFile, "key-file", "i", "", "path to ssh private key file")

	return cmd
}

type serverAddOptions struct {
	host    string
	user    string
	keyFile string
}

// TODO: if there is no cluster yet - add server. if config contains a node, new node should be in same cluster.
// ?? ask for manager node to be added first, if config empty
// TODO: leave flag - leave cluster to join
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

	// TODO: add only same cluster nodes.
	// if node is not in a cluster, join it first

	info, err := node.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}
	if info.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return errors.New("swarm mode is not enabled. please use cicdez server init")
	}

	manager, mhost, err := docker.GetManagerClient(ctx, config.Servers)
	if err != nil && !errors.Is(err, docker.ErrManagerNotFound) {
		return err
	}

	// if node is a worker, we should check if it's part of a cluster
	if !info.Info.Swarm.ControlAvailable {
		var inswarm bool
		for _, p := range info.Info.Swarm.RemoteManagers {
			host, _, err := net.SplitHostPort(p.Addr)
			if err != nil {
				return err
			}

			if host == mhost {
				inswarm = true
				break
			}
		}

		if !inswarm {
			return errors.New("worker must be part of a cluster")
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
		RemoteAddrs:   []string{mhost},
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

func newServerListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerList(cmd.OutOrStdout())
		},
	}
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

func newServerRemoveCommand() *cobra.Command {
	opts := serverRemoveOptions{}
	cmd := &cobra.Command{
		Use:     "remove HOST",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = args[0]
			return runServerRemove(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}

	return cmd
}

type serverRemoveOptions struct {
	name string
}

// TODO: force flag
// TODO: soft flag. remove server from config without leaving a cluster
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
	manager, _, err := docker.GetManagerClient(ctx, config.Servers, opts.name)
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
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	t := time.NewTicker(time.Second)
	defer t.Stop()

	filters := make(client.Filters)
	filters.Add("node", nodeID)
	filters.Add("desired-state", "running")

wait:
	for {
		select {
		case <-cctx.Done():
			return cctx.Err()
		case <-t.C:
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

func newServerInitCommand() *cobra.Command {
	var opts serverInitOptions
	cmd := &cobra.Command{
		Use:   "init HOST",
		Short: "Initialize a fresh server for Docker Swarm deployments",
		Long: `Provision a fresh server for Docker Swarm deployments.

This command connects to a server, creates a deployer user, installs Docker,
initializes a Docker Swarm, and saves the configuration.

Example:
  cicdez server init 192.168.1.100
  cicdez server init example.com -i ~/.ssh/id_ed25519`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.host = args[0]
			in, _ := cmd.InOrStdin().(*os.File)

			if !initSwarmMap[opts.swarm] {
				return fmt.Errorf("wrong swarm option: %s", opts.swarm)
			}

			return runServerInit(cmd.Context(), in, cmd.OutOrStdout(), opts)
		},
	}

	cmd.Flags().IntVarP(&opts.port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&opts.user, "user", "u", "root", "SSH user for initial connection")
	cmd.Flags().StringVarP(&opts.key, "key", "i", "", "path to SSH private key")
	cmd.Flags().BoolVar(&opts.disablePasswordAuth, "disable-password-auth", false, "disable SSH password auth after setup")
	cmd.Flags().StringVar(&opts.swarm, "swarm", InitSwarmManager, "")
	cmd.Flags().BoolVar(&opts.dockerUser, "with-docker-user", true, "")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print what would be done without making changes")

	return cmd
}

const (
	InitSwarmNone    = "none"
	InitSwarmManager = "manager"
	InitSwarmWorker  = "worker"
)

var initSwarmMap = map[string]bool{
	InitSwarmNone:    true,
	InitSwarmManager: true,
	InitSwarmWorker:  true,
}

type serverInitOptions struct {
	host                string
	port                int
	user                string
	key                 string
	disablePasswordAuth bool
	dockerUser          bool
	swarm               string
	dryRun              bool
}

func runServerInit(ctx context.Context, in *os.File, out io.Writer, opts serverInitOptions) error {
	homeDir, _ := os.UserHomeDir()
	rootKeyPath := filepath.Join(homeDir, ".ssh", opts.host+"-"+opts.user)

	if opts.key == "" {
		if _, err := os.Stat(rootKeyPath); err == nil {
			opts.key = rootKeyPath
		}
	}

	var client *gossh.Client
	var key []byte
	var err error

	fmt.Fprintf(out, "Connecting to %s@%s:%d...\n", opts.user, opts.host, opts.port)
	if opts.key != "" {
		key, err = os.ReadFile(opts.key)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		client, err = ssh.DialWithKey(opts.host, opts.port, opts.user, key)
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

	if len(key) == 0 {
		fmt.Fprintln(out, "Generating SSH key...")
		if !opts.dryRun {
			pkey, pub, err := ssh.GenerateEd25519KeyPair()
			if err != nil {
				return fmt.Errorf("failed to generate key: %w", err)
			}
			key = []byte(pkey)

			fmt.Fprintf(out, "Saving key to %s...\n", rootKeyPath)
			if err := os.WriteFile(rootKeyPath, key, 0o600); err != nil {
				return fmt.Errorf("failed to save key: %w", err)
			}
			if err := ensureAuthorizedKey(client, opts.user, pub); err != nil {
				return fmt.Errorf("failed to install key: %w", err)
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

	if opts.dockerUser {
		err = createDockerUser(client, out, opts.dryRun)
		if err != nil {
			return err
		}

		if server.User != DockerUser {
			server.Key = ""
		}
		server.User = DockerUser

		var public string
		if server.Key == "" {
			server.Key, public, err = ssh.GenerateEd25519KeyPair()
			if err != nil {
				return fmt.Errorf("failed to generate key: %w", err)
			}
		} else {
			signer, err := gossh.ParsePrivateKey([]byte(server.Key))
			if err != nil {
				return err
			}

			public = strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))
		}

		config.Servers[opts.host] = server
		if !opts.dryRun {
			if err := vault.SaveConfig(cwd, config); err != nil {
				return err
			}

			if err := ensureAuthorizedKey(client, server.User, public); err != nil {
				return err
			}
		}

	} else {
		server.User = opts.user
		server.Key = string(key)
		config.Servers[opts.host] = server

		if !opts.dryRun {
			if err := vault.SaveConfig(cwd, config); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
		}
	}

	if opts.disablePasswordAuth {
		fmt.Fprintln(out, "Disabling password authentication...")
		if !opts.dryRun {
			_, stderr, err := ssh.Run(client, "sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config && systemctl restart sshd", true)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
			}
		}
	}

	if err := setupDocker(client, out, server.User, opts.dryRun); err != nil {
		return err
	}

	if opts.swarm != InitSwarmNone {
		fmt.Fprintln(out, "Joining swarm...")
		if err := joinSwarm(ctx, opts.host, config.Servers, opts.swarm == InitSwarmWorker, opts.dryRun); err != nil {
			return err
		}
	}

	if opts.dryRun {
		fmt.Fprintln(out, "\nDry run complete. No changes were made.")
	} else {
		fmt.Fprintf(out, "\nServer '%s' initialized successfully.\n", opts.host)
	}

	return nil
}

const (
	DockerUser = "cicdez"
)

func createDockerUser(client *gossh.Client, out io.Writer, dry bool) error {
	_, stderr, err := ssh.Run(client, fmt.Sprintf("id %s", DockerUser), false)
	if err != nil {
		return err
	}
	if stderr != "" {
		fmt.Fprintf(out, "Creating %s user...\n", DockerUser)
		if !dry {
			_, stderr, err := ssh.Run(client, fmt.Sprintf("adduser --disabled-password --comment '' %s", DockerUser), true)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
			}
		}
	}

	return nil
}

func setupDocker(client *gossh.Client, out io.Writer, user string, dry bool) error {
	stdout, _, err := ssh.Run(client, "docker --version", true)
	if err != nil {
		return err
	}
	if stdout == "" {
		fmt.Fprintln(out, "Installing Docker...")
		if !dry {
			_, stderr, err := ssh.Run(client, "curl -fsSL https://get.docker.com | sh && systemctl enable docker.service && systemctl enable containerd.service", true)
			if err != nil || stderr != "" {
				return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
			}
		}
	}

	fmt.Fprintf(out, "Adding %s to docker group...\n", user)
	if !dry {
		_, stderr, err := ssh.Run(client, fmt.Sprintf("usermod -aG docker %s", user), true)
		if err != nil || stderr != "" {
			return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
		}
	}

	return nil
}

func joinSwarm(ctx context.Context, host string, servers map[string]vault.Server, worker, dryRun bool) error {
	server := servers[host]

	node, err := docker.NewClientSSH(host, server.Port, server.User, []byte(server.Key))
	if err != nil {
		return err
	}
	defer node.Close()

	info, err := node.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}
	manager, mhost, err := docker.GetManagerClient(ctx, servers, host)
	if errors.Is(err, docker.ErrManagerNotFound) {
		if worker {
			return errors.New("can't init worker without manager")
		}

		if info.Info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
			if !dryRun {
				_, err := node.SwarmInit(ctx, dockerclient.SwarmInitOptions{
					AdvertiseAddr: host,
				})
				if err != nil {
					return err
				}
			}
		}
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

	if !dryRun {
		if _, err := node.SwarmJoin(ctx, dockerclient.SwarmJoinOptions{
			AdvertiseAddr: host,
			RemoteAddrs:   []string{mhost},
			JoinToken:     token,
		}); err != nil {
			return err
		}
	}
	return nil
}

func ensureAuthorizedKey(client *gossh.Client, user, pubKey string) error {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	sshDir := home + "/.ssh"
	pubKey = strings.TrimSpace(pubKey)
	script := fmt.Sprintf(`mkdir -p %s && (grep -qF '%s' %s/authorized_keys 2>/dev/null || echo '%s' >> %s/authorized_keys) && chown -R %s:%s %s && chmod 700 %s && chmod 600 %s/authorized_keys`,
		sshDir, pubKey, sshDir, pubKey, sshDir, user, user, sshDir, sshDir, sshDir)
	_, stderr, err := ssh.Run(client, script, true)
	if err != nil || stderr != "" {
		return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
	}
	return nil
}
