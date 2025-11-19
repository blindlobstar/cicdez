package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strconv"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
	dockertypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
)

func loadCompose(ctx context.Context, env []string, paths ...string) (types.Project, error) {
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

func buildCompose(ctx context.Context, project types.Project, services ...string) error {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	serviceConfigs := make([]types.ServiceConfig, 0, len(project.Services))
	for _, service := range services {
		if sc, ok := project.Services[service]; ok {
			serviceConfigs = append(serviceConfigs, sc)
		} else {
			return fmt.Errorf("service %s is not presented in compose file", service)
		}
	}
	if len(serviceConfigs) == 0 {
		for _, service := range project.Services {
			serviceConfigs = append(serviceConfigs, service)
		}
	}

	for _, service := range serviceConfigs {
		if service.Build == nil {
			continue
		}

		buildContext := service.Build.Context
		if buildContext == "" {
			buildContext = "."
		}

		dockerfile := service.Build.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		tar, err := archive.TarWithOptions(buildContext, &archive.TarOptions{})
		if err != nil {
			return fmt.Errorf("failed to create build context for service %s: %w", service.Name, err)
		}

		buildOptions := build.ImageBuildOptions{
			Context:    tar,
			Dockerfile: dockerfile,
			Tags:       []string{service.Image},
			Remove:     true,
			BuildArgs:  service.Build.Args,
		}

		response, err := dockerClient.ImageBuild(ctx, buildOptions.Context, buildOptions)
		if err != nil {
			return fmt.Errorf("failed to build image for service %s: %w", service.Name, err)
		}
		defer response.Body.Close()

		_, err = io.Copy(os.Stdout, response.Body)
		if err != nil {
			return fmt.Errorf("error reading build output for service %s: %w", service.Name, err)
		}
	}

	return nil
}

func pushCompose(ctx context.Context, images []string, username, password string) error {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	authConfig := registry.AuthConfig{
		Username: username,
		Password: password,
	}
	encoded, err := json.Marshal(authConfig)
	if err != nil {
		return fmt.Errorf("failed to encode auth config: %w", err)
	}
	encodedAuth := base64.URLEncoding.EncodeToString(encoded)

	for _, image := range images {
		fmt.Printf("Pushing image: %s\n", image)

		response, err := dockerClient.ImagePush(ctx, image, dockertypes.PushOptions{
			RegistryAuth: encodedAuth,
		})
		if err != nil {
			return fmt.Errorf("failed to push image %s: %w", image, err)
		}
		defer response.Close()

		_, err = io.Copy(os.Stdout, response)
		if err != nil {
			return fmt.Errorf("error reading push output for image %s: %w", image, err)
		}
	}

	return nil
}

func deployCompose(ctx context.Context, project types.Project, stackName string, services ...string) error {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	networkIDs := make(map[string]string)
	for networkName, networkConfig := range project.Networks {
		fullNetworkName := fmt.Sprintf("%s_%s", stackName, networkName)

		networks, err := dockerClient.NetworkList(ctx, network.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list networks: %w", err)
		}

		var existingNetworkID string
		for _, net := range networks {
			if net.Name == fullNetworkName {
				existingNetworkID = net.ID
				break
			}
		}

		if existingNetworkID != "" {
			networkIDs[networkName] = existingNetworkID
		} else {
			networkCreateOptions := network.CreateOptions{
				Driver: networkConfig.Driver,
				Labels: map[string]string{
					"com.docker.stack.namespace": stackName,
				},
				Attachable: true,
			}

			if networkConfig.Driver == "overlay" || networkConfig.Driver == "" {
				networkCreateOptions.Driver = "overlay"
			}

			resp, err := dockerClient.NetworkCreate(ctx, fullNetworkName, networkCreateOptions)
			if err != nil {
				return fmt.Errorf("failed to create network %s: %w", fullNetworkName, err)
			}
			networkIDs[networkName] = resp.ID
		}
	}

	serviceConfigs := make([]types.ServiceConfig, 0, len(project.Services))
	for _, service := range services {
		if sc, ok := project.Services[service]; ok {
			serviceConfigs = append(serviceConfigs, sc)
		} else {
			return fmt.Errorf("service %s is not presented in compose file", service)
		}
	}
	if len(serviceConfigs) == 0 {
		for _, service := range project.Services {
			serviceConfigs = append(serviceConfigs, service)
		}
	}

	for _, service := range serviceConfigs {
		serviceName := fmt.Sprintf("%s_%s", stackName, service.Name)

		serviceSpec, err := convertToServiceSpec(service, stackName, networkIDs)
		if err != nil {
			return fmt.Errorf("failed to convert service %s: %w", service.Name, err)
		}

		existingServices, err := dockerClient.ServiceList(ctx, swarm.ServiceListOptions{
			Filters: filters.Args{},
			Status:  false,
		})
		if err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}

		var existingServiceID string
		for _, svc := range existingServices {
			if svc.Spec.Name == serviceName {
				existingServiceID = svc.ID
				break
			}
		}

		if existingServiceID != "" {
			fmt.Printf("Updating service: %s\n", serviceName)

			existingService, _, err := dockerClient.ServiceInspectWithRaw(ctx, existingServiceID, swarm.ServiceInspectOptions{})
			if err != nil {
				return fmt.Errorf("failed to inspect service %s: %w", serviceName, err)
			}

			_, err = dockerClient.ServiceUpdate(ctx, existingServiceID, existingService.Version, serviceSpec, swarm.ServiceUpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update service %s: %w", serviceName, err)
			}
		} else {
			fmt.Printf("Creating service: %s\n", serviceName)
			_, err = dockerClient.ServiceCreate(ctx, serviceSpec, swarm.ServiceCreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create service %s: %w", serviceName, err)
			}
		}
	}

	return nil
}

func convertToServiceSpec(service types.ServiceConfig, stackName string, networkIDs map[string]string) (swarm.ServiceSpec, error) {
	serviceName := fmt.Sprintf("%s_%s", stackName, service.Name)

	env := make([]string, 0)
	for key, value := range service.Environment {
		if value != nil {
			env = append(env, fmt.Sprintf("%s=%s", key, *value))
		}
	}

	ports := make([]swarm.PortConfig, 0)
	for _, port := range service.Ports {
		protocol := swarm.PortConfigProtocolTCP
		if port.Protocol != "" {
			protocol = swarm.PortConfigProtocol(port.Protocol)
		}

		pp64, err := strconv.ParseUint(port.Published, 10, 32)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("failed to parse publish port %s: %w", port.Published, err)
		}
		ports = append(ports, swarm.PortConfig{
			Protocol:      protocol,
			TargetPort:    port.Target,
			PublishedPort: uint32(pp64),
			PublishMode:   swarm.PortConfigPublishModeIngress,
		})
	}

	mounts := make([]mount.Mount, 0)
	for _, vol := range service.Volumes {
		mountType := mount.TypeVolume
		if vol.Type != "" {
			mountType = mount.Type(vol.Type)
		}

		mounts = append(mounts, mount.Mount{
			Type:   mountType,
			Source: vol.Source,
			Target: vol.Target,
		})
	}

	networks := make([]swarm.NetworkAttachmentConfig, 0)
	for networkName := range service.Networks {
		if netID, ok := networkIDs[networkName]; ok {
			networks = append(networks, swarm.NetworkAttachmentConfig{
				Target: netID,
			})
		}
	}

	replicas := uint64(1)
	if service.Deploy != nil && service.Deploy.Replicas != nil {
		replicas = uint64(*service.Deploy.Replicas)
	}

	containerSpec := &swarm.ContainerSpec{
		Image:  service.Image,
		Env:    env,
		Mounts: mounts,
	}

	if len(service.Command) > 0 {
		containerSpec.Command = service.Command
	}

	taskTemplate := swarm.TaskSpec{
		ContainerSpec: containerSpec,
		Networks:      networks,
	}

	serviceSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"com.docker.stack.namespace": stackName,
				"com.docker.stack.image":     service.Image,
			},
		},
		TaskTemplate: taskTemplate,
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{
				Replicas: &replicas,
			},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: ports,
		},
	}

	maps.Copy(serviceSpec.Labels, service.Labels)

	return serviceSpec, nil
}
