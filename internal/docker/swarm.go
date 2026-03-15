package docker

import (
	"context"
	"errors"
	"slices"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/moby/moby/client"
)

var ErrManagerNotFound = errors.New("manager not found")

func GetManagerClient(ctx context.Context, servers map[string]vault.Server, exclude ...string) (client.APIClient, error) {
	for host, server := range servers {
		if slices.Contains(exclude, host) {
			continue
		}
		manager, err := NewClientSSH(host, server.Port, server.User, []byte(server.Key))
		if err != nil {
			return nil, err
		}

		info, err := manager.Info(ctx, client.InfoOptions{})
		if err != nil {
			manager.Close()
			return nil, err
		}
		if !info.Info.Swarm.ControlAvailable {
			manager.Close()
			continue
		}
		return manager, nil
	}
	return nil, ErrManagerNotFound
}
