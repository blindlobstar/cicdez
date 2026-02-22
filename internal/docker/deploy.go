package docker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/blindlobstar/cicdez/internal/vault"
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
	ResolveImageAlways  = "always"
	ResolveImageChanged = "changed"
	ResolveImageNever   = "never"
)

type DeployOptions struct {
	Secrets      vault.Secrets
	Stack        string
	Prune        bool
	ResolveImage string
	Quiet        bool
	Registries   map[string]registry.AuthConfig
	Out          io.Writer
}

func Deploy(ctx context.Context, dockerClient client.APIClient, project types.Project, opts DeployOptions) error {
	if err := processLocalConfigs(&project); err != nil {
		return fmt.Errorf("failed to process local configs: %w", err)
	}

	if err := processSensitiveSecrets(&project, opts.Secrets); err != nil {
		return fmt.Errorf("failed to process sensitive secrets: %w", err)
	}

	if err := checkDaemonIsSwarmManager(ctx, dockerClient); err != nil {
		return err
	}

	if opts.Prune {
		services := map[string]struct{}{}
		for _, svc := range project.Services {
			services[svc.Name] = struct{}{}
		}
		if err := pruneServices(ctx, dockerClient, opts.Stack, services); err != nil {
			return err
		}
	}

	serviceNetworks := GetServicesDeclaredNetworks(project.Services)
	networks, externalNetworks := ConvertNetworks(opts.Stack, project.Networks, serviceNetworks)
	if err := validateExternalNetworks(ctx, dockerClient, externalNetworks); err != nil {
		return err
	}
	if err := createNetworks(ctx, dockerClient, opts.Stack, networks); err != nil {
		return err
	}

	secrets, err := ConvertSecrets(opts.Stack, project.Secrets)
	if err != nil {
		return err
	}
	if err := createSecrets(ctx, dockerClient, secrets); err != nil {
		return err
	}

	configs, err := ConvertConfigs(opts.Stack, project.Configs)
	if err != nil {
		return err
	}
	if err := createConfigs(ctx, dockerClient, configs); err != nil {
		return err
	}

	services, err := ConvertServices(ctx, dockerClient, opts.Stack, project)
	if err != nil {
		return err
	}

	_, err = deployServices(ctx, dockerClient, services, opts.Stack, opts.ResolveImage, opts.Registries, opts.Quiet, opts.Out)
	if err != nil {
		return err
	}

	return nil
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
	return make(client.Filters).Add("label", LabelNamespace+"="+stack)
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
			createOpts.Driver = DefaultNetworkDriver
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

func deployServices(ctx context.Context, apiClient client.APIClient, services map[string]swarm.ServiceSpec, stack string, resolveImage string, registries map[string]registry.AuthConfig, quiet bool, out io.Writer) ([]string, error) {
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
		name := ScopeName(stack, internalName)
		image := serviceSpec.TaskTemplate.ContainerSpec.Image

		encodedAuth := getEncodedAuth(image, registries)

		if svc, exists := existingServiceMap[name]; exists {
			updateOpts := client.ServiceUpdateOptions{
				Version:             svc.Version,
				EncodedRegistryAuth: encodedAuth,
			}

			switch resolveImage {
			case ResolveImageAlways:
				updateOpts.QueryRegistry = true
			case ResolveImageChanged:
				if image != svc.Spec.Labels[LabelImage] {
					updateOpts.QueryRegistry = true
				} else {
					serviceSpec.TaskTemplate.ContainerSpec.Image = svc.Spec.TaskTemplate.ContainerSpec.Image
				}
			default:
				if image == svc.Spec.Labels[LabelImage] {
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
				fmt.Fprintf(out, "Updating service %s\n", name)
			}

			serviceIDs = append(serviceIDs, svc.ID)
		} else {
			if !quiet {
				fmt.Fprintf(out, "Creating service %s\n", name)
			}

			queryRegistry := resolveImage == ResolveImageAlways || resolveImage == ResolveImageChanged

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

func HasBuildConfig(project types.Project) bool {
	for _, svc := range project.Services {
		if svc.Build != nil {
			return true
		}
	}
	return false
}

func hashedName(name string, content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%s_%s", name, hex.EncodeToString(hash[:])[:8])
}

func processLocalConfigs(project *types.Project) error {
	if project.Configs == nil {
		project.Configs = make(types.Configs)
	}

	for svcName, svc := range project.Services {
		for name, localConfig := range svc.LocalConfigs {
			sourcePath := localConfig.Source
			if !filepath.IsAbs(sourcePath) {
				sourcePath = filepath.Join(project.WorkingDir, sourcePath)
			}

			content, err := os.ReadFile(sourcePath)
			if err != nil {
				return fmt.Errorf("failed to read local config file %s for service %s: %w", localConfig.Source, svc.Name, err)
			}

			configName := hashedName(name, content)
			project.Configs[configName] = types.ConfigObjConfig{
				Content: string(content),
			}

			svc.Configs = append(svc.Configs, types.ServiceConfigObjConfig{
				Source: configName,
				Target: localConfig.Target,
			})
		}
		project.Services[svcName] = svc
	}

	return nil
}

func processSensitiveSecrets(project *types.Project, allSecrets vault.Secrets) error {
	if project.Secrets == nil {
		project.Secrets = make(types.Secrets)
	}

	for svcName, svc := range project.Services {
		for name, sensitive := range svc.Sensitive {
			content, err := formatSensitiveSecrets(allSecrets, sensitive, project.WorkingDir)
			if err != nil {
				return fmt.Errorf("failed to format sensitive secrets for service %s target %s: %w", svc.Name, sensitive.Target, err)
			}

			secretName := hashedName(name, content)
			project.Secrets[secretName] = types.SecretConfig{
				Content: string(content),
			}

			svc.Secrets = append(svc.Secrets, types.ServiceSecretConfig{
				Source: secretName,
				Target: sensitive.Target,
				UID:    sensitive.UID,
				GID:    sensitive.GID,
				Mode:   sensitive.Mode,
			})
		}
		project.Services[svcName] = svc
	}

	return nil
}

func formatSensitiveSecrets(allSecrets vault.Secrets, sensitive types.SensitiveConfig, cwd string) ([]byte, error) {
	switch sensitive.Format {
	case vault.SecretOutputEnv, "":
		return vault.FormatEnv(allSecrets, sensitive.Secrets)
	case vault.SecretOutputJSON:
		return vault.FormatJSON(allSecrets, sensitive.Secrets)
	case vault.SecretOutputRaw:
		return vault.FormatRaw(allSecrets, sensitive.Secrets)
	case vault.SecretOutputTemplate:
		templatePath := sensitive.Template
		if templatePath == "" {
			return nil, fmt.Errorf("template format requires template path")
		}
		if !filepath.IsAbs(templatePath) {
			templatePath = filepath.Join(cwd, templatePath)
		}
		data, err := os.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read template file %s: %w", sensitive.Template, err)
		}
		return vault.FormatTemplate(allSecrets, sensitive.Secrets, string(data))
	default:
		return nil, fmt.Errorf("unknown format: %s", sensitive.Format)
	}
}
