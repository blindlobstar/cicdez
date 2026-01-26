package main

import (
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize cicdez in the current directory",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	var identity *age.X25519Identity

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	e, err := NewEncrypter(cwd)
	if err != nil {
		return err
	}

	keyPath, err := getKeyPath()
	if err != nil {
		return fmt.Errorf("failed to get key path: %w", err)
	}

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		newIdentity, err := age.GenerateX25519Identity()
		if err != nil {
			return fmt.Errorf("failed to generate age key: %w", err)
		}
		identity = newIdentity

		keyDir := filepath.Dir(keyPath)
		if err := os.MkdirAll(keyDir, 0o700); err != nil {
			return fmt.Errorf("failed to create key directory: %w", err)
		}

		if err := os.WriteFile(keyPath, []byte(identity.String()+"\n"), 0o600); err != nil {
			return fmt.Errorf("failed to write age key: %w", err)
		}

		fmt.Printf("Generated new age key at %s\n\n", keyPath)
	} else {
		if err := e.LoadIdentity(); err != nil {
			return err
		}
		if i, ok := e.Identity.(*age.X25519Identity); !ok {
			return fmt.Errorf("failed to cast identity to X25519Identity")
		} else {
			identity = i
		}
		fmt.Printf("Using existing age key at %s\n\n", keyPath)
	}

	publicKey := identity.Recipient().String()
	fmt.Printf("Your public key (share with team):\n  %s\n\n", publicKey)

	if err := os.MkdirAll(cicdezDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .cicdez directory: %w", err)
	}

	fullRecipientsPath := filepath.Join(cwd, recipientsPath)
	if _, err := os.Stat(fullRecipientsPath); os.IsNotExist(err) {
		if err := os.WriteFile(fullRecipientsPath, []byte(""), 0o644); err != nil {
			return fmt.Errorf("failed to create recipients.txt: %w", err)
		}
	}

	if err := e.AddRecipient(publicKey); err != nil {
		if err.Error() != "recipient already exists" {
			return fmt.Errorf("failed to add recipient: %w", err)
		}
	}

	fullConfigPath := filepath.Join(cwd, configPath)
	if _, err := os.Stat(fullConfigPath); os.IsNotExist(err) {
		if err := e.EncryptFile([]byte("servers: {}\nregistries: {}\n"), filepath.Join(cwd, configPath)); err != nil {
			return fmt.Errorf("failed to create config.age: %w", err)
		}
	}

	fullSecretsPath := filepath.Join(cwd, secretsPath)
	if _, err := os.Stat(fullSecretsPath); os.IsNotExist(err) {
		if err := e.EncryptFile([]byte("values: {}\n"), filepath.Join(cwd, secretsPath)); err != nil {
			return fmt.Errorf("failed to create secrets.age: %w", err)
		}
	}

	fmt.Println("cicdez initialized")
	return nil
}
