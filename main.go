package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const cicdezDir = ".cicdez"

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cicdez",
		Short: "Easy deployment and continuous delivery tool using Docker Swarm, SOPS, and age encryption",
		Long: `cicdez simplifies deployment management by:
- Managing secrets with age encryption
- Extending Docker Compose with git context and custom features
- Deploying to Docker Swarm with version control
- Tracking configuration changes via git`,
	}
	cmd.AddCommand(newSecretCommand())
	cmd.AddCommand(newServerCommand())
	cmd.AddCommand(newRegistryCommand())
	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newDeployCommand())
	return cmd
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
