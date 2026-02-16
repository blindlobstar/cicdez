package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/moby/moby/client"
	"golang.org/x/crypto/ssh"
)

func NewClientSSH(host, user string, privateKey []byte) (client.APIClient, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		addr = host + ":22"
	}

	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial ssh: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return sshClient.Dial("unix", "/var/run/docker.sock")
			},
		},
	}

	return client.New(
		client.WithHTTPClient(httpClient),
	)
}
