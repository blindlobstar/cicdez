package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cicdez",
		Short: "Manage deployments, configuration, and secrets",
		Long: `Build images, manage encrypted secrets, and deploy to Docker Swarm.
Secrets and credentials are encrypted with age and stored locally.`,
	}
	cmd.AddCommand(NewKeyCommand())
	cmd.AddCommand(NewSecretCommand())
	cmd.AddCommand(NewServerCommand())
	cmd.AddCommand(NewRegistryCommand())
	cmd.AddCommand(NewBuildCommand())
	cmd.AddCommand(NewDeployCommand())
	return cmd
}
