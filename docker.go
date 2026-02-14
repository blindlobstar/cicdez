package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/go-archive"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"golang.org/x/crypto/ssh"
)

const (
	labelNamespace       = "com.docker.stack.namespace"
	labelImage           = "com.docker.stack.image"
	defaultNetworkDriver = "overlay"
	resolveImageAlways   = "always"
	resolveImageChanged  = "changed"
	resolveImageNever    = "never"
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

func NewDockerClientSSH(host, user string, privateKey []byte) (client.APIClient, error) {
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

	sshClient, err := ssh.Dial("tcp", host+":22", sshConfig)
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
		client.WithHost("http://docker"),
	)
}

type BuildOptions struct {
	services   map[string]bool
	cwd        string
	registries map[string]registry.AuthConfig
}

func Build(ctx context.Context, dockerClient client.APIClient, project types.Project, opt BuildOptions) error {
	for _, svc := range project.Services {
		if len(opt.services) > 0 && !opt.services[svc.Name] {
			continue
		}

		if svc.Build == nil {
			continue
		}

		imageName := svc.Image
		if imageName == "" {
			imageName = project.Name + "_" + svc.Name
		}

		fmt.Printf("Building %s...\n", imageName)

		if err := buildImage(ctx, dockerClient, opt.cwd, imageName, svc.Build); err != nil {
			return fmt.Errorf("failed to build %s: %w", svc.Name, err)
		}

		if buildPush {
			fmt.Printf("Pushing %s...\n", imageName)
			if err := pushImage(ctx, dockerClient, imageName, opt.registries); err != nil {
				return fmt.Errorf("failed to push %s: %w", svc.Name, err)
			}
		}
	}

	return nil
}

type DeployOptions struct {
	stack        string
	prune        bool
	resolveImage string
	detach       bool
	quiet        bool
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
		pruneServices(ctx, dockerClient, opts.stack, services)
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

	services, err := convertServices(opts.stack, project)
	if err != nil {
		return err
	}

	serviceIDs, err := deployServices(ctx, dockerClient, services, opts.stack, opts.resolveImage, opts.quiet)
	if err != nil {
		return err
	}

	if opts.detach {
		return nil
	}

	return waitOnServices(ctx, dockerClient, serviceIDs, opts.quiet)
}

func buildImage(ctx context.Context, dockerClient client.APIClient, cwd, imageName string, build *types.BuildConfig) error {
	buildContext := build.Context
	if buildContext == "" {
		buildContext = "."
	}

	if !filepath.IsAbs(buildContext) {
		buildContext = filepath.Join(cwd, buildContext)
	}

	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	buildContextReader, err := archive.TarWithOptions(buildContext, &archive.TarOptions{
		ExcludePatterns: []string{".git"},
	})
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}

	buildArgs := make(map[string]*string)
	for k, v := range build.Args {
		if v != nil {
			val := *v
			buildArgs[k] = &val
		} else {
			buildArgs[k] = nil
		}
	}

	opts := client.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: dockerfile,
		BuildArgs:  buildArgs,
		NoCache:    buildNoCache || build.NoCache,
		PullParent: buildPull || build.Pull,
		Remove:     true,
		Target:     build.Target,
	}

	resp, err := dockerClient.ImageBuild(ctx, buildContextReader, opts)
	if err != nil {
		return fmt.Errorf("failed to start build: %w", err)
	}
	defer resp.Body.Close()

	return jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, os.Stdout.Fd(), true, nil)
}

func pushImage(ctx context.Context, dockerClient client.APIClient, imageName string, registries map[string]registry.AuthConfig) error {
	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return err
	}

	var registryHost string
	if reference.Domain(ref) == "" {
		registryHost = "https://index.docker.io/v1/"
	}

	var authStr string
	if auth, ok := registries[registryHost]; ok {
		authBytes, err := json.Marshal(auth)
		if err != nil {
			return fmt.Errorf("failed to encode auth: %w", err)
		}
		authStr = base64.URLEncoding.EncodeToString(authBytes)
	}

	opts := client.ImagePushOptions{
		RegistryAuth: authStr,
	}

	resp, err := dockerClient.ImagePush(ctx, imageName, opts)
	if err != nil {
		return fmt.Errorf("failed to start push: %w", err)
	}
	defer resp.Close()

	return jsonmessage.DisplayJSONMessagesStream(resp, os.Stdout, os.Stdout.Fd(), true, nil)
}

// scopeName adds the stack namespace prefix to a name
func scopeName(stack, name string) string {
	return stack + "_" + name
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
	for k, v := range labels {
		result[k] = v
	}
	result[labelNamespace] = stack
	return result
}

func createSecrets(ctx context.Context, apiClient client.APIClient, secrets []swarm.SecretSpec) error {
	for _, secretSpec := range secrets {
		res, err := apiClient.SecretInspect(ctx, secretSpec.Name, client.SecretInspectOptions{})
		switch {
		case err == nil:
			_, err := apiClient.SecretUpdate(ctx, res.Secret.ID, client.SecretUpdateOptions{
				Version: res.Secret.Meta.Version,
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
				Version: res.Config.Meta.Version,
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

func deployServices(ctx context.Context, apiClient client.APIClient, services map[string]swarm.ServiceSpec, stack string, resolveImage string, quiet bool) ([]string, error) {
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

		if svc, exists := existingServiceMap[name]; exists {
			updateOpts := client.ServiceUpdateOptions{
				Version: svc.Version,
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
				Spec:          serviceSpec,
				QueryRegistry: queryRegistry,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create service %s: %w", name, err)
			}

			serviceIDs = append(serviceIDs, response.ID)
		}
	}

	return serviceIDs, nil
}

func waitOnServices(ctx context.Context, dockerClient client.APIClient, serviceIDs []string, quiet bool) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for _, serviceID := range serviceIDs {
		if !quiet {
			fmt.Fprintf(os.Stdout, "Waiting for service %s to converge...\n", serviceID)
		}

	waitLoop:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timeout:
				return fmt.Errorf("timeout waiting for service %s to converge", serviceID)
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
						break waitLoop
					}
				}
			}
		}
	}

	return nil
}
