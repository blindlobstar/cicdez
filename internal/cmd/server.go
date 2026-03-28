package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

	return cmd
}

func newServerAddCommand() *cobra.Command {
	opts := serverAddOptions{port: 22}
	cmd := &cobra.Command{
		Use:   "add HOST",
		Short: "Add or update a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.host = args[0]
			if _, ok := addSwarmMap[opts.role]; !ok {
				return fmt.Errorf("role %s is not supported", opts.role)
			}
			return runServerAdd(cmd.Context(), cmd.InOrStdin().(*os.File), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().IntVarP(&opts.port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVarP(&opts.user, "user", "u", "root", "SSH user")
	cmd.Flags().StringVarP(&opts.keyFile, "key-file", "i", "", "path to SSH private key file")
	cmd.Flags().BoolVar(&opts.setup, "setup", false, "provision fresh server")
	cmd.Flags().StringVar(&opts.role, "role", AddSwarmManager, "role in swarm")
	cmd.Flags().BoolVar(&opts.disablePasswordAuth, "disable-password-auth", false, "disable SSH password auth (requires --setup)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print what would be done without making changes")

	return cmd
}

const (
	AddSwarmManager = "manager"
	AddSwarmWorker  = "worker"
)

var addSwarmMap = map[string]bool{
	AddSwarmManager: true,
	AddSwarmWorker:  true,
}

type serverAddOptions struct {
	host                string
	port                int
	user                string
	keyFile             string
	role                string
	setup               bool
	disablePasswordAuth bool
	dryRun              bool
}

// TODO: leave flag - leave cluster to join
func runServerAdd(ctx context.Context, in *os.File, out io.Writer, opts serverAddOptions) error {
	server := vault.Server{
		User: opts.user,
		Port: opts.port,
	}

	if opts.keyFile != "" {
		data, err := os.ReadFile(opts.keyFile)
		if err != nil {
			return fmt.Errorf("failed to read key file: %w", err)
		}
		server.Key = data
	}

	if opts.setup {
		homeDir, _ := os.UserHomeDir()
		rootKeyPath := filepath.Join(homeDir, ".ssh", opts.host+"-"+server.User)

		if _, err := os.Stat(rootKeyPath); err == nil && len(server.Key) == 0 {
			server.Key, err = os.ReadFile(rootKeyPath)
			if err != nil {
				return fmt.Errorf("failed to read key file: %w", err)
			}
		}

		var client *gossh.Client
		var err error

		fmt.Fprintf(out, "Connecting to %s@%s:%d...\n", server.User, opts.host, server.Port)
		if len(server.Key) > 0 {
			client, err = ssh.DialWithKey(opts.host, server.Port, server.User, server.Key)
		} else {
			fmt.Fprintf(out, "Enter password for %s: ", opts.user)

			passwordBytes, err := term.ReadPassword(int(in.Fd()))
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}
			password := string(passwordBytes)

			client, err = ssh.DialWithPassword(opts.host, server.Port, server.User, password)
		}
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer client.Close()

		if len(server.Key) == 0 {
			fmt.Fprintln(out, "Generating SSH key...")
			if !opts.dryRun {
				var public []byte
				server.Key, public, err = ssh.GenerateEd25519KeyPair()
				if err != nil {
					return fmt.Errorf("failed to generate key: %w", err)
				}

				fmt.Fprintf(out, "Saving key to %s...\n", rootKeyPath)
				if err := os.WriteFile(rootKeyPath, server.Key, 0o600); err != nil {
					return fmt.Errorf("failed to save key: %w", err)
				}
				if err := ensureAuthorizedKey(client, server.User, public); err != nil {
					return fmt.Errorf("failed to install key: %w", err)
				}
			}
		}

		err = createDockerUser(client, out, opts.dryRun)
		if err != nil {
			return err
		}

		server.User = DockerUser

		var public []byte
		server.Key, public, err = ssh.GenerateEd25519KeyPair()
		if err != nil {
			return fmt.Errorf("failed to generate key: %w", err)
		}

		if !opts.dryRun {
			if err := ensureAuthorizedKey(client, server.User, public); err != nil {
				return err
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
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	config, err := vault.LoadConfig(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	node, err := docker.NewClientSSH(opts.host, server.Port, server.User, server.Key)
	if err != nil {
		return err
	}

	info, err := node.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return err
	}

	if info.Info.Swarm.LocalNodeState == swarm.LocalNodeStateActive {
		var clusterId string
		for host, server := range config.Servers {
			node, err := docker.NewClientSSH(host, server.Port, server.User, server.Key)
			if err != nil {
				return err
			}

			info, err := node.Info(ctx, dockerclient.InfoOptions{})
			if err != nil {
				return err
			}
			clusterId = info.Info.Swarm.Cluster.ID
			break
		}

		if clusterId != "" && info.Info.Swarm.Cluster.ID != clusterId {
			return errors.New("node in the different cluster")
		}
	} else if len(config.Servers) == 0 {
		if !opts.dryRun {
			_, err := node.SwarmInit(ctx, dockerclient.SwarmInitOptions{
				AdvertiseAddr: opts.host,
			})
			if err != nil {
				return err
			}
		}
	} else {
		manager, mhost, err := docker.GetManagerClient(ctx, config.Servers)
		if err != nil {
			return err
		}

		inspect, err := manager.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
		if err != nil {
			return err
		}

		token := inspect.Swarm.JoinTokens.Manager
		if opts.role == AddSwarmWorker {
			token = inspect.Swarm.JoinTokens.Worker
		}

		if !opts.dryRun {
			if _, err := node.SwarmJoin(ctx, dockerclient.SwarmJoinOptions{
				AdvertiseAddr: opts.host,
				RemoteAddrs:   []string{mhost},
				JoinToken:     token,
			}); err != nil {
				return err
			}
		}
	}

	config.Servers[opts.host] = server
	if !opts.dryRun {
		if err := vault.SaveConfig(cwd, config); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
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
		if len(server.Key) > 0 {
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
	delete(config.Servers, opts.name)

	node, err := docker.NewClientSSH(opts.name, server.Port, server.User, server.Key)
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
	manager, _, err := docker.GetManagerClient(ctx, config.Servers)
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

	if err := vault.SaveConfig(cwd, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(out, "Server '%s' removed\n", opts.name)
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

func ensureAuthorizedKey(client *gossh.Client, user string, public []byte) error {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	sshDir := home + "/.ssh"

	public = bytes.TrimSpace(public)
	script := fmt.Sprintf(`mkdir -p %s && (grep -qF '%s' %s/authorized_keys 2>/dev/null || echo '%s' >> %s/authorized_keys) && chown -R %s:%s %s && chmod 700 %s && chmod 600 %s/authorized_keys`,
		sshDir, public, sshDir, public, sshDir, user, user, sshDir, sshDir, sshDir)
	_, stderr, err := ssh.Run(client, script, true)
	if err != nil || stderr != "" {
		return errors.New(strings.Join([]string{err.Error(), stderr}, "\n"))
	}
	return nil
}
