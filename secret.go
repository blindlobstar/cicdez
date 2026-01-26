package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage encrypted secrets",
	Long:  "Add, list, edit, and remove encrypted secrets stored in .cicdez/secrets.age",
}

var secretAddCmd = &cobra.Command{
	Use:   "add <name> <value>",
	Short: "Add or update a secret",
	Args:  cobra.ExactArgs(2),
	RunE:  runSecretAdd,
}

var secretListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all secret names",
	RunE:    runSecretList,
}

var secretEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit all secrets using $EDITOR",
	RunE:  runSecretEdit,
}

var secretRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a secret",
	Args:    cobra.ExactArgs(1),
	RunE:    runSecretRemove,
}

func init() {
	secretCmd.AddCommand(secretAddCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretEditCmd)
	secretCmd.AddCommand(secretRemoveCmd)
}

func runSecretAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	value := args[1]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	secrets, err := loadSecrets(e, cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if secrets.Values == nil {
		secrets.Values = make(map[string]string)
	}

	secrets.Values[name] = value

	if err := saveSecrets(e, cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Printf("Secret '%s' added\n", name)
	return nil
}

func runSecretList(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	secrets, err := loadSecrets(e, cwd)
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

func runSecretEdit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	secrets, err := loadSecrets(e, cwd)
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

	var editedSecrets Secrets
	if err := yaml.Unmarshal(editedData, &editedSecrets); err != nil {
		return fmt.Errorf("failed to parse edited secrets: %w", err)
	}

	if err := saveSecrets(e, cwd, editedSecrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Println("Secrets updated")
	return nil
}

func runSecretRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return fmt.Errorf("failed to create encrypter: %w", err)
	}

	secrets, err := loadSecrets(e, cwd)
	if err != nil {
		return fmt.Errorf("failed to load secrets: %w", err)
	}

	if _, exists := secrets.Values[name]; !exists {
		return fmt.Errorf("secret '%s' not found", name)
	}

	delete(secrets.Values, name)

	if err := saveSecrets(e, cwd, secrets); err != nil {
		return fmt.Errorf("failed to save secrets: %w", err)
	}

	fmt.Printf("Secret '%s' removed\n", name)
	return nil
}
