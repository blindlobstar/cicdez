package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const cicdezDir = ".cicdez"

var rootCmd = &cobra.Command{
	Use:   "cicdez",
	Short: "Easy deployment and continuous delivery tool using Docker Swarm, SOPS, and age encryption",
	Long: `cicdez simplifies deployment management by:
- Managing secrets with age encryption
- Extending Docker Compose with git context and custom features
- Deploying to Docker Swarm with version control
- Tracking configuration changes via git`,
}

func main() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(secretCmd)
	rootCmd.AddCommand(serverCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
