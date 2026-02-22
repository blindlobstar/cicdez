package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/spf13/cobra"
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

	addOpts := secretAddOptions{}
	addCmd := &cobra.Command{
		Use:   "add NAME VALUE",
		Short: "Add or update a secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			addOpts.name = args[0]
			addOpts.value = args[1]
			return runSecretAdd(cmd.OutOrStdout(), addOpts)
		},
	}

	removeOpts := secretRemoveOptions{}
	removeCmd := &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removeOpts.name = args[0]
			return runSecretRemove(cmd.OutOrStdout(), removeOpts)
		},
	}

	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List secret names",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretList(cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Edit secrets using $EDITOR",
		Long: `Decrypt secrets, open in editor, and re-encrypt after saving.

Secrets are written to a temporary YAML file and opened in $EDITOR.
Falls back to vim if $EDITOR is not set.
The temporary file is deleted after the editor exits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretEdit(cmd.OutOrStdout())
		},
	})
	cmd.AddCommand(removeCmd)

	return cmd
}

func runSecretAdd(out io.Writer, opts secretAddOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if secrets == nil {
		secrets = make(vault.Secrets)
	}

	secrets[opts.name] = opts.value

	if err := vault.SaveSecrets(cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Fprintf(out, "Secret '%s' added\n", opts.name)
	return nil
}

func runSecretList(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if len(secrets) == 0 {
		fmt.Fprintln(out, "No secrets found")
		return nil
	}

	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(out, "Secrets:")
	for _, name := range names {
		fmt.Fprintf(out, "  %s\n", name)
	}

	return nil
}

func runSecretEdit(out io.Writer) error {
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

	editedSecrets, err := vault.ParseSecrets(editedData)
	if err != nil {
		return fmt.Errorf("failed to parse edited secrets: %w", err)
	}

	if err := vault.SaveSecrets(cwd, editedSecrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Fprintln(out, "Secrets updated")
	return nil
}

func runSecretRemove(out io.Writer, opts secretRemoveOptions) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	secrets, err := vault.LoadSecrets(cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if _, exists := secrets[opts.name]; !exists {
		return fmt.Errorf("secret '%s' not found", opts.name)
	}

	delete(secrets, opts.name)

	if err := vault.SaveSecrets(cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Fprintf(out, "Secret '%s' removed\n", opts.name)
	return nil
}
