package ssh

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const defaultTimeout = 30 * time.Second

func DialWithKey(host string, port int, user string, keyData []byte) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         defaultTimeout,
	}

	return dial(host, port, config)
}

func DialWithPassword(host string, port int, user, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         defaultTimeout,
	}

	return dial(host, port, config)
}

func dial(host string, port int, config *ssh.ClientConfig) (*ssh.Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	return client, nil
}

func Run(client *ssh.Client, cmd string, sudo bool) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if sudo {
		cmd = fmt.Sprintf("sudo sh -c %q", cmd)
	}

	err = session.Run(cmd)
	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())

	if err != nil {
		return stdout, stderr, fmt.Errorf("%w: %s", err, stderr)
	}

	return stdout, stderr, nil
}

// RunWithAgent runs cmd with an in-memory ssh agent forwarded to the remote
// host, so the command can ssh onward using keys that never land on remote disk.
func RunWithAgent(client *ssh.Client, cmd string, keys ...[]byte) (string, string, error) {
	keyring := agent.NewKeyring()
	for _, keyData := range keys {
		key, err := ssh.ParseRawPrivateKey(keyData)
		if err != nil {
			return "", "", fmt.Errorf("failed to parse private key: %w", err)
		}
		if err := keyring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
			return "", "", fmt.Errorf("failed to add key to agent: %w", err)
		}
	}

	if err := agent.ForwardToAgent(client, keyring); err != nil {
		return "", "", fmt.Errorf("failed to set up agent forwarding: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	if err := agent.RequestAgentForwarding(session); err != nil {
		return "", "", fmt.Errorf("failed to request agent forwarding: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	err = session.Run(cmd)
	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())

	if err != nil {
		return stdout, stderr, fmt.Errorf("%w: %s", err, stderr)
	}

	return stdout, stderr, nil
}
