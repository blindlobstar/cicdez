package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strconv"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const (
	labelNamespace       = "com.docker.stack.namespace"
	labelImage           = "com.docker.stack.image"
	defaultNetworkDriver = "overlay"
)

func LoadCompose(ctx context.Context, env []string, paths ...string) (types.Project, error) {
	projectOptions, err := cli.NewProjectOptions(
		paths,
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithEnv(env),
		cli.WithInterpolation(true),
	)
	if err != nil {
		return types.Project{}, fmt.Errorf("failed to create project options: %w", err)
	}
	composeProject, err := projectOptions.LoadProject(ctx)
	if err != nil {
		return types.Project{}, fmt.Errorf("error to load project: %w", err)
	}

	return *composeProject, nil
}

// scopeName adds the stack namespace prefix to a name
func scopeName(stack, name string) string {
	return stack + "_" + name
}

func getServicesDeclaredNetworks(serviceConfigs types.Services) map[string]struct{} {
	serviceNetworks := map[string]struct{}{}
	for _, serviceConfig := range serviceConfigs {
		if len(serviceConfig.Networks) == 0 {
			serviceNetworks["default"] = struct{}{}
			continue
		}
		for nw := range serviceConfig.Networks {
			serviceNetworks[nw] = struct{}{}
		}
	}
	return serviceNetworks
}

func convertNetworks(stack string, networks types.Networks, serviceNetworks map[string]struct{}) (map[string]client.NetworkCreateOptions, []string) {
	result := make(map[string]client.NetworkCreateOptions)
	var externalNetworks []string

	for name, net := range networks {
		if _, used := serviceNetworks[name]; !used {
			continue
		}

		if bool(net.External) {
			extName := net.Name
			if extName == "" {
				extName = name
			}
			externalNetworks = append(externalNetworks, extName)
			continue
		}

		netName := scopeName(stack, name)
		if net.Name != "" {
			netName = net.Name
		}

		opts := client.NetworkCreateOptions{
			Labels:     addStackLabel(stack, net.Labels),
			Driver:     net.Driver,
			Options:    net.DriverOpts,
			Attachable: net.Attachable,
		}

		if net.Ipam.Driver != "" {
			opts.IPAM = &network.IPAM{
				Driver: net.Ipam.Driver,
			}
		}

		if opts.Driver == "" {
			opts.Driver = defaultNetworkDriver
		}

		result[netName] = opts
	}

	return result, externalNetworks
}

func convertSecrets(stack string, secrets types.Secrets) ([]swarm.SecretSpec, error) {
	var result []swarm.SecretSpec

	for name, secret := range secrets {
		if bool(secret.External) {
			continue
		}

		secretName := scopeName(stack, name)
		if secret.Name != "" {
			secretName = secret.Name
		}

		var data []byte
		var err error
		if secret.File != "" {
			data, err = os.ReadFile(secret.File)
			if err != nil {
				return nil, fmt.Errorf("failed to read secret file %s: %w", secret.File, err)
			}
		} else if secret.Content != "" {
			data = []byte(secret.Content)
		}

		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   secretName,
				Labels: addStackLabel(stack, secret.Labels),
			},
			Data: data,
		}
		result = append(result, spec)
	}

	return result, nil
}

func convertConfigs(stack string, configs types.Configs) ([]swarm.ConfigSpec, error) {
	var result []swarm.ConfigSpec

	for name, config := range configs {
		if bool(config.External) {
			continue
		}

		configName := scopeName(stack, name)
		if config.Name != "" {
			configName = config.Name
		}

		var data []byte
		var err error
		if config.File != "" {
			data, err = os.ReadFile(config.File)
			if err != nil {
				return nil, fmt.Errorf("failed to read config file %s: %w", config.File, err)
			}
		} else if config.Content != "" {
			data = []byte(config.Content)
		}

		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   configName,
				Labels: addStackLabel(stack, config.Labels),
			},
			Data: data,
		}
		result = append(result, spec)
	}

	return result, nil
}

func convertServices(stack string, project types.Project) (map[string]swarm.ServiceSpec, error) {
	result := make(map[string]swarm.ServiceSpec)

	for _, svc := range project.Services {
		spec, err := convertService(stack, svc, project.Networks, project.Secrets, project.Configs)
		if err != nil {
			return nil, fmt.Errorf("failed to convert service %s: %w", svc.Name, err)
		}
		result[svc.Name] = spec
	}

	return result, nil
}

func convertService(stack string, svc types.ServiceConfig, networks types.Networks, secrets types.Secrets, configs types.Configs) (swarm.ServiceSpec, error) {
	labels := addStackLabel(stack, svc.Labels)
	labels[labelImage] = svc.Image

	containerSpec := &swarm.ContainerSpec{
		Image:      svc.Image,
		Command:    svc.Entrypoint,
		Args:       svc.Command,
		Hostname:   svc.Hostname,
		Dir:        svc.WorkingDir,
		User:       svc.User,
		StopSignal: svc.StopSignal,
		TTY:        svc.Tty,
		OpenStdin:  svc.StdinOpen,
		ReadOnly:   svc.ReadOnly,
	}

	if svc.Environment != nil {
		containerSpec.Env = make([]string, 0, len(svc.Environment))
		for k, v := range svc.Environment {
			if v != nil {
				containerSpec.Env = append(containerSpec.Env, k+"="+*v)
			}
		}
	}

	for _, vol := range svc.Volumes {
		m := mount.Mount{
			Source:   vol.Source,
			Target:   vol.Target,
			Type:     mount.Type(vol.Type),
			ReadOnly: vol.ReadOnly,
		}
		containerSpec.Mounts = append(containerSpec.Mounts, m)
	}

	for _, secretRef := range svc.Secrets {
		secret, ok := secrets[secretRef.Source]
		if !ok {
			return swarm.ServiceSpec{}, fmt.Errorf("secret %s not found", secretRef.Source)
		}

		secretName := scopeName(stack, secretRef.Source)
		if secret.Name != "" {
			secretName = secret.Name
		} else if secret.External {
			secretName = secretRef.Source
		}

		target := secretRef.Target
		if target == "" {
			target = secretRef.Source
		}

		var mode os.FileMode = 0o444
		if secretRef.Mode != nil {
			mode = os.FileMode(*secretRef.Mode)
		}

		containerSpec.Secrets = append(containerSpec.Secrets, &swarm.SecretReference{
			SecretName: secretName,
			File: &swarm.SecretReferenceFileTarget{
				Name: target,
				UID:  secretRef.UID,
				GID:  secretRef.GID,
				Mode: mode,
			},
		})
	}

	for _, configRef := range svc.Configs {
		config, ok := configs[configRef.Source]
		if !ok {
			return swarm.ServiceSpec{}, fmt.Errorf("config %s not found", configRef.Source)
		}

		configName := scopeName(stack, configRef.Source)
		if config.Name != "" {
			configName = config.Name
		} else if config.External {
			configName = configRef.Source
		}

		target := configRef.Target
		if target == "" {
			target = "/" + configRef.Source
		}

		var mode os.FileMode = 0o444
		if configRef.Mode != nil {
			mode = os.FileMode(*configRef.Mode)
		}

		containerSpec.Configs = append(containerSpec.Configs, &swarm.ConfigReference{
			ConfigName: configName,
			File: &swarm.ConfigReferenceFileTarget{
				Name: target,
				UID:  configRef.UID,
				GID:  configRef.GID,
				Mode: mode,
			},
		})
	}

	var networkAttachments []swarm.NetworkAttachmentConfig
	if len(svc.Networks) == 0 {
		networkAttachments = append(networkAttachments, swarm.NetworkAttachmentConfig{
			Target:  scopeName(stack, "default"),
			Aliases: []string{svc.Name},
		})
	} else {
		for netName, netConfig := range svc.Networks {
			target := scopeName(stack, netName)
			if net, ok := networks[netName]; ok && net.Name != "" {
				target = net.Name
			}
			if net, ok := networks[netName]; ok && bool(net.External) {
				target = net.Name
				if target == "" {
					target = netName
				}
			}

			aliases := []string{svc.Name}
			if netConfig != nil && len(netConfig.Aliases) > 0 {
				aliases = append(aliases, netConfig.Aliases...)
			}

			networkAttachments = append(networkAttachments, swarm.NetworkAttachmentConfig{
				Target:  target,
				Aliases: aliases,
			})
		}
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   scopeName(stack, svc.Name),
			Labels: labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: containerSpec,
			Networks:      networkAttachments,
		},
	}

	if svc.Deploy != nil {
		if svc.Deploy.Replicas != nil {
			replicas := uint64(*svc.Deploy.Replicas)
			spec.Mode = swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: &replicas,
				},
			}
		}

		if svc.Deploy.UpdateConfig != nil {
			spec.UpdateConfig = &swarm.UpdateConfig{}
			if svc.Deploy.UpdateConfig.Parallelism != nil {
				spec.UpdateConfig.Parallelism = *svc.Deploy.UpdateConfig.Parallelism
			}
			if svc.Deploy.UpdateConfig.Delay != 0 {
				spec.UpdateConfig.Delay = time.Duration(svc.Deploy.UpdateConfig.Delay)
			}
			if svc.Deploy.UpdateConfig.FailureAction != "" {
				spec.UpdateConfig.FailureAction = swarm.FailureAction(svc.Deploy.UpdateConfig.FailureAction)
			}
			if svc.Deploy.UpdateConfig.Order != "" {
				spec.UpdateConfig.Order = swarm.UpdateOrder(svc.Deploy.UpdateConfig.Order)
			}
		}

		if svc.Deploy.RestartPolicy != nil {
			spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{}
			if svc.Deploy.RestartPolicy.Condition != "" {
				condition := swarm.RestartPolicyCondition(svc.Deploy.RestartPolicy.Condition)
				spec.TaskTemplate.RestartPolicy.Condition = condition
			}
			if svc.Deploy.RestartPolicy.Delay != nil {
				delay := time.Duration(*svc.Deploy.RestartPolicy.Delay)
				spec.TaskTemplate.RestartPolicy.Delay = &delay
			}
			if svc.Deploy.RestartPolicy.MaxAttempts != nil {
				spec.TaskTemplate.RestartPolicy.MaxAttempts = svc.Deploy.RestartPolicy.MaxAttempts
			}
			if svc.Deploy.RestartPolicy.Window != nil {
				window := time.Duration(*svc.Deploy.RestartPolicy.Window)
				spec.TaskTemplate.RestartPolicy.Window = &window
			}
		}

		if len(svc.Deploy.Placement.Constraints) > 0 {
			spec.TaskTemplate.Placement = &swarm.Placement{
				Constraints: svc.Deploy.Placement.Constraints,
			}
		}
	}

	for _, port := range svc.Ports {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}

		var publishedPort uint32
		if port.Published != "" {
			p, err := strconv.ParseUint(port.Published, 10, 32)
			if err == nil {
				publishedPort = uint32(p)
			}
		}

		portConfig := swarm.PortConfig{
			TargetPort:    port.Target,
			PublishedPort: publishedPort,
			Protocol:      network.IPProtocol(port.Protocol),
		}
		if port.Mode == "host" {
			portConfig.PublishMode = swarm.PortConfigPublishModeHost
		} else {
			portConfig.PublishMode = swarm.PortConfigPublishModeIngress
		}
		spec.EndpointSpec.Ports = append(spec.EndpointSpec.Ports, portConfig)
	}

	return spec, nil
}

func addStackLabel(stack string, labels types.Labels) map[string]string {
	result := make(map[string]string)
	maps.Copy(result, labels)
	result[labelNamespace] = stack
	return result
}
