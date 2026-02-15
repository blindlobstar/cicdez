package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const (
	resolveImageAlways  = "always"
	resolveImageChanged = "changed"
	resolveImageNever   = "never"
)

type DeployOptions struct {
	stack        string
	prune        bool
	resolveImage string
	detach       bool
	quiet        bool
	registries   map[string]registry.AuthConfig
}

func Deploy(ctx context.Context, dockerClient client.APIClient, project types.Project, opts DeployOptions) error {
	if err := checkDaemonIsSwarmManager(ctx, dockerClient); err != nil {
		return err
	}

	if opts.prune {
		services := map[string]struct{}{}
		for _, svc := range project.Services {
			services[svc.Name] = struct{}{}
		}
		if err := pruneServices(ctx, dockerClient, opts.stack, services); err != nil {
			return err
		}
	}

	serviceNetworks := getServicesDeclaredNetworks(project.Services)
	networks, externalNetworks := convertNetworks(opts.stack, project.Networks, serviceNetworks)
	if err := validateExternalNetworks(ctx, dockerClient, externalNetworks); err != nil {
		return err
	}
	if err := createNetworks(ctx, dockerClient, opts.stack, networks); err != nil {
		return err
	}

	secrets, err := convertSecrets(opts.stack, project.Secrets)
	if err != nil {
		return err
	}
	if err := createSecrets(ctx, dockerClient, secrets); err != nil {
		return err
	}

	configs, err := convertConfigs(opts.stack, project.Configs)
	if err != nil {
		return err
	}
	if err := createConfigs(ctx, dockerClient, configs); err != nil {
		return err
	}

	services, err := convertServices(ctx, dockerClient, opts.stack, project)
	if err != nil {
		return err
	}

	serviceIDs, err := deployServices(ctx, dockerClient, services, opts.stack, opts.resolveImage, opts.registries, opts.quiet)
	if err != nil {
		return err
	}

	if opts.detach {
		return nil
	}

	return waitOnServices(ctx, dockerClient, serviceIDs, opts.quiet)
}

func checkDaemonIsSwarmManager(ctx context.Context, dockerClient client.APIClient) error {
	res, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return err
	}
	if !res.Info.Swarm.ControlAvailable {
		return errors.New(`this node is not a swarm manager. Use "docker swarm init" or "docker swarm join" to connect this node to swarm and try again`)
	}
	return nil
}

func getStackFilter(stack string) client.Filters {
	return make(client.Filters).Add("label", labelNamespace+"="+stack)
}

func pruneServices(ctx context.Context, dockerClient client.APIClient, stack string, services map[string]struct{}) error {
	res, err := dockerClient.ServiceList(ctx, client.ServiceListOptions{Filters: getStackFilter(stack)})
	if err != nil {
		return err
	}

	var pruneErr error
	for _, svc := range res.Items {
		if _, exists := services[svc.Spec.Name]; !exists {
			if _, err := dockerClient.ServiceRemove(ctx, svc.ID, client.ServiceRemoveOptions{}); err != nil {
				pruneErr = errors.Join(pruneErr, err)
			}
		}
	}
	return pruneErr
}

func validateExternalNetworks(ctx context.Context, apiClient client.APIClient, externalNetworks []string) error {
	for _, networkName := range externalNetworks {
		if !container.NetworkMode(networkName).IsUserDefined() {
			continue
		}
		res, err := apiClient.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
		switch {
		case errdefs.IsNotFound(err):
			return fmt.Errorf("network %q is declared as external, but could not be found. You need to create a swarm-scoped network before the stack is deployed", networkName)
		case err != nil:
			return err
		case res.Network.Scope != "swarm":
			return fmt.Errorf("network %q is declared as external, but it is not in the right scope: %q instead of \"swarm\"", networkName, res.Network.Scope)
		}
	}
	return nil
}

func createNetworks(ctx context.Context, apiClient client.APIClient, stack string, networks map[string]client.NetworkCreateOptions) error {
	res, err := apiClient.NetworkList(ctx, client.NetworkListOptions{Filters: getStackFilter(stack)})
	if err != nil {
		return err
	}

	existingNetworkMap := make(map[string]network.Summary)
	for _, nw := range res.Items {
		existingNetworkMap[nw.Name] = nw
	}

	for name, createOpts := range networks {
		if _, exists := existingNetworkMap[name]; exists {
			continue
		}

		if createOpts.Driver == "" {
			createOpts.Driver = defaultNetworkDriver
		}

		if _, err := apiClient.NetworkCreate(ctx, name, createOpts); err != nil {
			return fmt.Errorf("failed to create network %s: %w", name, err)
		}
	}
	return nil
}

func createSecrets(ctx context.Context, apiClient client.APIClient, secrets []swarm.SecretSpec) error {
	for _, secretSpec := range secrets {
		res, err := apiClient.SecretInspect(ctx, secretSpec.Name, client.SecretInspectOptions{})
		switch {
		case err == nil:
			_, err := apiClient.SecretUpdate(ctx, res.Secret.ID, client.SecretUpdateOptions{
				Version: res.Secret.Version,
				Spec:    secretSpec,
			})
			if err != nil {
				return fmt.Errorf("failed to update secret %s: %w", secretSpec.Name, err)
			}
		case errdefs.IsNotFound(err):
			_, err := apiClient.SecretCreate(ctx, client.SecretCreateOptions{
				Spec: secretSpec,
			})
			if err != nil {
				return fmt.Errorf("failed to create secret %s: %w", secretSpec.Name, err)
			}
		default:
			return err
		}
	}
	return nil
}

func createConfigs(ctx context.Context, apiClient client.APIClient, configs []swarm.ConfigSpec) error {
	for _, configSpec := range configs {
		res, err := apiClient.ConfigInspect(ctx, configSpec.Name, client.ConfigInspectOptions{})
		switch {
		case err == nil:
			_, err := apiClient.ConfigUpdate(ctx, res.Config.ID, client.ConfigUpdateOptions{
				Version: res.Config.Version,
				Spec:    configSpec,
			})
			if err != nil {
				return fmt.Errorf("failed to update config %s: %w", configSpec.Name, err)
			}
		case errdefs.IsNotFound(err):
			_, err := apiClient.ConfigCreate(ctx, client.ConfigCreateOptions{
				Spec: configSpec,
			})
			if err != nil {
				return fmt.Errorf("failed to create config %s: %w", configSpec.Name, err)
			}
		default:
			return err
		}
	}
	return nil
}

func deployServices(ctx context.Context, apiClient client.APIClient, services map[string]swarm.ServiceSpec, stack string, resolveImage string, registries map[string]registry.AuthConfig, quiet bool) ([]string, error) {
	res, err := apiClient.ServiceList(ctx, client.ServiceListOptions{Filters: getStackFilter(stack)})
	if err != nil {
		return nil, err
	}

	existingServiceMap := make(map[string]swarm.Service)
	for _, svc := range res.Items {
		existingServiceMap[svc.Spec.Name] = svc
	}

	var serviceIDs []string

	for internalName, serviceSpec := range services {
		name := scopeName(stack, internalName)
		image := serviceSpec.TaskTemplate.ContainerSpec.Image

		// Get encoded registry auth for the image
		encodedAuth := getEncodedAuth(image, registries)

		if svc, exists := existingServiceMap[name]; exists {
			updateOpts := client.ServiceUpdateOptions{
				Version:             svc.Version,
				EncodedRegistryAuth: encodedAuth,
			}

			switch resolveImage {
			case resolveImageAlways:
				updateOpts.QueryRegistry = true
			case resolveImageChanged:
				if image != svc.Spec.Labels[labelImage] {
					updateOpts.QueryRegistry = true
				} else {
					serviceSpec.TaskTemplate.ContainerSpec.Image = svc.Spec.TaskTemplate.ContainerSpec.Image
				}
			default:
				if image == svc.Spec.Labels[labelImage] {
					serviceSpec.TaskTemplate.ContainerSpec.Image = svc.Spec.TaskTemplate.ContainerSpec.Image
				}
			}

			serviceSpec.TaskTemplate.ForceUpdate = svc.Spec.TaskTemplate.ForceUpdate
			updateOpts.Spec = serviceSpec

			_, err := apiClient.ServiceUpdate(ctx, svc.ID, updateOpts)
			if err != nil {
				return nil, fmt.Errorf("failed to update service %s: %w", name, err)
			}

			if !quiet {
				fmt.Fprintf(os.Stdout, "Updating service %s\n", name)
			}

			serviceIDs = append(serviceIDs, svc.ID)
		} else {
			if !quiet {
				fmt.Fprintf(os.Stdout, "Creating service %s\n", name)
			}

			queryRegistry := resolveImage == resolveImageAlways || resolveImage == resolveImageChanged

			response, err := apiClient.ServiceCreate(ctx, client.ServiceCreateOptions{
				Spec:                serviceSpec,
				EncodedRegistryAuth: encodedAuth,
				QueryRegistry:       queryRegistry,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create service %s: %w", name, err)
			}

			serviceIDs = append(serviceIDs, response.ID)
		}
	}

	return serviceIDs, nil
}

func getEncodedAuth(image string, registries map[string]registry.AuthConfig) string {
	if len(registries) == 0 {
		return ""
	}

	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return ""
	}

	registryHost := reference.Domain(ref)
	auth, ok := registries[registryHost]
	if !ok {
		return ""
	}

	authBytes, err := json.Marshal(auth)
	if err != nil {
		return ""
	}

	return base64.URLEncoding.EncodeToString(authBytes)
}

func waitOnServices(ctx context.Context, dockerClient client.APIClient, serviceIDs []string, quiet bool) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for _, serviceID := range serviceIDs {
		if !quiet {
			fmt.Fprintf(os.Stdout, "Waiting for service %s to converge...\n", serviceID)
		}

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				res, err := dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
				if err != nil {
					return fmt.Errorf("failed to inspect service %s: %w", serviceID, err)
				}

				if res.Service.ServiceStatus != nil {
					running := res.Service.ServiceStatus.RunningTasks
					desired := res.Service.ServiceStatus.DesiredTasks
					if running >= desired && desired > 0 {
						if !quiet {
							fmt.Fprintf(os.Stdout, "Service %s converged (%d/%d tasks running)\n", serviceID, running, desired)
						}
						break
					}
				}
			}
		}
	}

	return nil
}
