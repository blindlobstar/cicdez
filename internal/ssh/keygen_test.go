package ssh

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateEd25519KeyPair(t *testing.T) {
	privateKey, publicKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("GenerateEd25519KeyPair failed: %v", err)
	}

	// Check private key format
	if !strings.HasPrefix(privateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Error("private key should start with PEM header")
	}
	if !strings.HasSuffix(strings.TrimSpace(privateKey), "-----END OPENSSH PRIVATE KEY-----") {
		t.Error("private key should end with PEM footer")
	}

	// Check public key format
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") {
		t.Error("public key should be in ssh-ed25519 format")
	}

	// Verify the private key can be parsed
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		t.Fatalf("failed to parse generated private key: %v", err)
	}

	// Verify the public keys match
	generatedPubKey := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	if strings.TrimSpace(generatedPubKey) != strings.TrimSpace(publicKey) {
		t.Error("public key from private key doesn't match returned public key")
	}
}

func TestGenerateEd25519KeyPair_Uniqueness(t *testing.T) {
	key1, _, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("first key generation failed: %v", err)
	}

	key2, _, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("second key generation failed: %v", err)
	}

	if key1 == key2 {
		t.Error("generated keys should be unique")
	}
}
