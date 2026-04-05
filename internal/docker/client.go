package docker

import (
	"context"
	"net"
	"net/http"

	"github.com/blindlobstar/cicdez/internal/ssh"
	"github.com/moby/moby/client"
)

func NewClientSSH(host string, port int, user string, privateKey []byte) (client.APIClient, error) {
	sshClient, err := ssh.DialWithKey(host, port, user, privateKey)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return sshClient.Dial("unix", "/var/run/docker.sock")
			},
		},
	}

	client, err := client.New(client.WithHTTPClient(httpClient))
	if err != nil {
		sshClient.Close()
		return nil, err
	}

	return client, nil
}
