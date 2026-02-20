package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
	"github.com/blindlobstar/cicdez/internal/vault"
	"gopkg.in/yaml.v3"
)

type secretAddOptions struct {
	name  string
	value string
}

type secretRemoveOptions struct {
	name string
}

func NewSecretCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage encrypted secrets",
	}

	addOpts := &secretAddOptions{}
	addCmd := &cobra.Command{
		Use:   "add <name> <value>",
		Short: "Add or update a secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.name = args[0]
			addOpts.value = args[1]
			return runSecretAdd(addOpts)
		},
	}

	removeOpts := &secretRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.name = args[0]
			return runSecretRemove(removeOpts)
		},
	}

	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all secret names",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretList()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Edit all secrets using $EDITOR",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretEdit()
		},
	})
	cmd.AddCommand(removeCmd)

	return cmd
}

func runSecretAdd(opts *secretAddOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if secrets.Values == nil {
		secrets.Values = make(map[string]string)
	}

	secrets.Values[opts.name] = opts.value

	if err := vault.SaveSecrets(cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Printf("Secret '%s' added\n", opts.name)
	return nil
}

func runSecretList() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if len(secrets.Values) == 0 {
		fmt.Println("No secrets found")
		return nil
	}

	names := make([]string, 0, len(secrets.Values))
	for name := range secrets.Values {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Secrets:")
	for _, name := range names {
		fmt.Printf("  %s\n", name)
	}

	return nil
}

func runSecretEdit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	data, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "cicdez-secrets-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("failed to run editor: %w", err)
	}

	editedData, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	var editedSecrets vault.Secrets
	if err := yaml.Unmarshal(editedData, &editedSecrets); err != nil {
		return fmt.Errorf("failed to parse edited secrets: %w", err)
	}

	if err := vault.SaveSecrets(cwd, editedSecrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Println("Secrets updated")
	return nil
}

func runSecretRemove(opts *secretRemoveOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if _, exists := secrets.Values[opts.name]; !exists {
		return fmt.Errorf("secret '%s' not found", opts.name)
	}

	delete(secrets.Values, opts.name)

	if err := vault.SaveSecrets(cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Printf("Secret '%s' removed\n", opts.name)
	return nil
}
