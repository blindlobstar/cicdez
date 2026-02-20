package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/spf13/cobra"
	"github.com/blindlobstar/cicdez/internal/vault"
)

type keyGenerateOptions struct {
	force      bool
	outputPath string
}

func NewKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage age encryption keys",
	}

	genOpts := keyGenerateOptions{}
	genCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new age encryption key",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeyGenerate(genOpts)
		},
	}
	genCmd.Flags().BoolVarP(&genOpts.force, "force", "f", false, "Overwrite existing key file")
	genCmd.Flags().StringVarP(&genOpts.outputPath, "output", "o", "", "Output path for the key file")

	cmd.AddCommand(genCmd)
	return cmd
}

func runKeyGenerate(opts keyGenerateOptions) error {
	if opts.outputPath == "" {
		var err error
		opts.outputPath, err = vault.GetKeyPath()
		if err != nil {
			return fmt.Errorf("failed to determine key path: %w", err)
		}
	}

	if _, err := os.Stat(opts.outputPath); err == nil {
		if !opts.force {
			return fmt.Errorf("key file already exists at %s (use --force to overwrite)", opts.outputPath)
		}
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("failed to generate age key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(opts.outputPath), 0o700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	keyContent := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n",
		time.Now().Format(time.RFC3339),
		identity.Recipient().String(),
		identity.String(),
	)

	if err := os.WriteFile(opts.outputPath, []byte(keyContent), 0o600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	fmt.Printf("Key generated successfully at %s\n", opts.outputPath)
	fmt.Printf("Public key: %s\n", identity.Recipient().String())
	return nil
}
