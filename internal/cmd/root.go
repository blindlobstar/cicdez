package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cicdez",
		Short: "Easy deployment and continuous delivery tool using Docker Swarm, SOPS, and age encryption",
		Long: `cicdez simplifies deployment management by:
- Managing secrets with age encryption
- Extending Docker Compose with git context and custom features
- Deploying to Docker Swarm with version control
- Tracking configuration changes via git`,
	}
	cmd.AddCommand(NewSecretCommand())
	cmd.AddCommand(NewServerCommand())
	cmd.AddCommand(NewRegistryCommand())
	cmd.AddCommand(NewBuildCommand())
	cmd.AddCommand(NewDeployCommand())
	return cmd
}
