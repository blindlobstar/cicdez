package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

func GenerateEd25519KeyPair() (privateKeyPEM, publicKeyOpenSSH string, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create ssh public key: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	privateKeyPEM = string(pem.EncodeToMemory(pemBlock))
	publicKeyOpenSSH = string(ssh.MarshalAuthorizedKey(sshPubKey))

	return privateKeyPEM, publicKeyOpenSSH, nil
}
