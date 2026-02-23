package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const (
	LabelNamespace       = "com.docker.stack.namespace"
	LabelImage           = "com.docker.stack.image"
	DefaultNetworkDriver = "overlay"
)

func LoadCompose(ctx context.Context, paths ...string) (types.Project, error) {
	projectOptions, err := cli.NewProjectOptions(
		paths,
		cli.WithOsEnv,
		cli.WithDotEnv,
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

// ScopeName adds the stack namespace prefix to a name
func ScopeName(stack, name string) string {
	return stack + "_" + name
}

func GetServicesDeclaredNetworks(serviceConfigs types.Services) map[string]struct{} {
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

func ConvertNetworks(stack string, networks types.Networks, serviceNetworks map[string]struct{}) (map[string]client.NetworkCreateOptions, []string) {
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

		netName := ScopeName(stack, name)
		if net.Name != "" {
			netName = net.Name
		}

		opts := client.NetworkCreateOptions{
			Labels:     AddStackLabel(stack, net.Labels),
			Driver:     net.Driver,
			Options:    net.DriverOpts,
			Internal:   net.Internal,
			Attachable: net.Attachable,
		}

		if net.Ipam.Driver != "" || len(net.Ipam.Config) > 0 {
			opts.IPAM = &network.IPAM{
				Driver: net.Ipam.Driver,
			}
			for _, ipamConfig := range net.Ipam.Config {
				sn, _ := netip.ParsePrefix(ipamConfig.Subnet)
				opts.IPAM.Config = append(opts.IPAM.Config, network.IPAMConfig{
					Subnet: sn,
				})
			}
		}

		if opts.Driver == "" {
			opts.Driver = DefaultNetworkDriver
		}

		result[netName] = opts
	}

	return result, externalNetworks
}

func ConvertSecrets(stack string, secrets types.Secrets) ([]swarm.SecretSpec, error) {
	var result []swarm.SecretSpec

	for name, secret := range secrets {
		if bool(secret.External) {
			continue
		}

		secretName := ScopeName(stack, name)
		if secret.Name != "" {
			secretName = secret.Name
		}

		var data []byte
		var err error
		if secret.Driver == "" {
			if secret.File != "" {
				data, err = os.ReadFile(secret.File)
				if err != nil {
					return nil, fmt.Errorf("failed to read secret file %s: %w", secret.File, err)
				}
			} else if secret.Content != "" {
				data = []byte(secret.Content)
			}
		}

		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   secretName,
				Labels: AddStackLabel(stack, secret.Labels),
			},
			Data: data,
		}

		if secret.Driver != "" {
			spec.Driver = &swarm.Driver{
				Name:    secret.Driver,
				Options: secret.DriverOpts,
			}
		}
		if secret.TemplateDriver != "" {
			spec.Templating = &swarm.Driver{
				Name: secret.TemplateDriver,
			}
		}

		result = append(result, spec)
	}

	return result, nil
}

func ConvertConfigs(stack string, configs types.Configs) ([]swarm.ConfigSpec, error) {
	var result []swarm.ConfigSpec

	for name, config := range configs {
		if bool(config.External) {
			continue
		}

		configName := ScopeName(stack, name)
		if config.Name != "" {
			configName = config.Name
		}

		var data []byte
		var err error
		if config.Driver == "" {
			if config.File != "" {
				data, err = os.ReadFile(config.File)
				if err != nil {
					return nil, fmt.Errorf("failed to read config file %s: %w", config.File, err)
				}
			} else if config.Content != "" {
				data = []byte(config.Content)
			}
		}

		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   configName,
				Labels: AddStackLabel(stack, config.Labels),
			},
			Data: data,
		}

		if config.TemplateDriver != "" {
			spec.Templating = &swarm.Driver{
				Name: config.TemplateDriver,
			}
		}

		result = append(result, spec)
	}

	return result, nil
}

func ConvertServices(ctx context.Context, apiClient client.APIClient, stack string, project types.Project) (map[string]swarm.ServiceSpec, error) {
	result := make(map[string]swarm.ServiceSpec)

	for _, svc := range project.Services {
		spec, err := convertService(ctx, apiClient, stack, svc, project.Networks, project.Volumes, project.Secrets, project.Configs)
		if err != nil {
			return nil, fmt.Errorf("failed to convert service %s: %w", svc.Name, err)
		}
		result[svc.Name] = spec
	}

	return result, nil
}

func convertService(ctx context.Context, apiClient client.APIClient, stack string, svc types.ServiceConfig, networks types.Networks, volumes types.Volumes, secrets types.Secrets, configs types.Configs) (swarm.ServiceSpec, error) {
	var deployLabels types.Labels
	if svc.Deploy != nil {
		deployLabels = svc.Deploy.Labels
	}
	serviceLabels := AddStackLabel(stack, deployLabels)
	serviceLabels[LabelImage] = svc.Image

	healthcheck, err := convertHealthcheck(svc.HealthCheck)
	if err != nil {
		return swarm.ServiceSpec{}, err
	}

	var stopGracePeriod *time.Duration
	if svc.StopGracePeriod != nil {
		d := time.Duration(*svc.StopGracePeriod)
		stopGracePeriod = &d
	}

	capAdd, capDrop := effectiveCapAddCapDrop(svc.CapAdd, svc.CapDrop)

	containerSpec := &swarm.ContainerSpec{
		Image:           svc.Image,
		Command:         svc.Entrypoint,
		Args:            svc.Command,
		Hostname:        svc.Hostname,
		Hosts:           convertExtraHosts(svc.ExtraHosts),
		DNSConfig:       convertDNSConfig(svc.DNS, svc.DNSSearch),
		Healthcheck:     healthcheck,
		Labels:          AddStackLabel(stack, svc.Labels),
		Dir:             svc.WorkingDir,
		User:            svc.User,
		StopGracePeriod: stopGracePeriod,
		StopSignal:      svc.StopSignal,
		TTY:             svc.Tty,
		OpenStdin:       svc.StdinOpen,
		ReadOnly:        svc.ReadOnly,
		Isolation:       container.Isolation(svc.Isolation),
		Init:            svc.Init,
		Sysctls:         svc.Sysctls,
		CapabilityAdd:   capAdd,
		CapabilityDrop:  capDrop,
		Ulimits:         convertUlimits(svc.Ulimits),
		OomScoreAdj:     svc.OomScoreAdj,
	}

	if svc.CredentialSpec != nil {
		credentialSpec, credConfigRef, err := convertCredentialSpec(ctx, apiClient, stack, *svc.CredentialSpec, configs)
		if err != nil {
			return swarm.ServiceSpec{}, err
		}
		containerSpec.Privileges = &swarm.Privileges{
			CredentialSpec: credentialSpec,
		}
		if credConfigRef != nil {
			containerSpec.Configs = append(containerSpec.Configs, credConfigRef)
		}
	}

	if svc.Environment != nil {
		containerSpec.Env = make([]string, 0, len(svc.Environment))
		for k, v := range svc.Environment {
			if v == nil {
				containerSpec.Env = append(containerSpec.Env, k)
			} else {
				containerSpec.Env = append(containerSpec.Env, k+"="+*v)
			}
		}
		sort.Strings(containerSpec.Env)
	}

	for _, vol := range svc.Volumes {
		m, err := convertVolumeToMount(vol, volumes, stack)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("volume %s: %w", vol.Source, err)
		}
		containerSpec.Mounts = append(containerSpec.Mounts, m)
	}

	for _, secretRef := range svc.Secrets {
		secret, ok := secrets[secretRef.Source]
		if !ok {
			return swarm.ServiceSpec{}, fmt.Errorf("secret %s not found", secretRef.Source)
		}

		secretName := ScopeName(stack, secretRef.Source)
		if secret.Name != "" {
			secretName = secret.Name
		} else if secret.External {
			secretName = secretRef.Source
		}

		secretID, err := lookupSecretID(ctx, apiClient, secretName)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("secret %s: %w", secretName, err)
		}

		target := secretRef.Target
		if target == "" {
			target = secretRef.Source
		}

		var mode os.FileMode = 0o444
		if secretRef.Mode != nil {
			mode = os.FileMode(*secretRef.Mode)
		}

		uid := secretRef.UID
		if uid == "" {
			uid = "0"
		}
		gid := secretRef.GID
		if gid == "" {
			gid = "0"
		}

		containerSpec.Secrets = append(containerSpec.Secrets, &swarm.SecretReference{
			SecretID:   secretID,
			SecretName: secretName,
			File: &swarm.SecretReferenceFileTarget{
				Name: target,
				UID:  uid,
				GID:  gid,
				Mode: mode,
			},
		})
	}

	for _, configRef := range svc.Configs {
		config, ok := configs[configRef.Source]
		if !ok {
			return swarm.ServiceSpec{}, fmt.Errorf("config %s not found", configRef.Source)
		}

		configName := ScopeName(stack, configRef.Source)
		if config.Name != "" {
			configName = config.Name
		} else if config.External {
			configName = configRef.Source
		}

		configID, err := lookupConfigID(ctx, apiClient, configName)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("config %s: %w", configName, err)
		}

		target := configRef.Target
		if target == "" {
			target = "/" + configRef.Source
		}

		var mode os.FileMode = 0o444
		if configRef.Mode != nil {
			mode = os.FileMode(*configRef.Mode)
		}

		uid := configRef.UID
		if uid == "" {
			uid = "0"
		}
		gid := configRef.GID
		if gid == "" {
			gid = "0"
		}

		containerSpec.Configs = append(containerSpec.Configs, &swarm.ConfigReference{
			ConfigID:   configID,
			ConfigName: configName,
			File: &swarm.ConfigReferenceFileTarget{
				Name: target,
				UID:  uid,
				GID:  gid,
				Mode: mode,
			},
		})
	}

	var networkAttachments []swarm.NetworkAttachmentConfig
	if len(svc.Networks) == 0 {
		networkAttachments = append(networkAttachments, swarm.NetworkAttachmentConfig{
			Target:  ScopeName(stack, "default"),
			Aliases: []string{svc.Name},
		})
	} else {
		for netName, netConfig := range svc.Networks {
			networkConfig, ok := networks[netName]
			if !ok && netName != "default" {
				return swarm.ServiceSpec{}, fmt.Errorf("undefined network %q", netName)
			}

			target := ScopeName(stack, netName)
			if networkConfig.Name != "" {
				target = networkConfig.Name
			}
			if bool(networkConfig.External) {
				target = networkConfig.Name
				if target == "" {
					target = netName
				}
			}

			var aliases []string
			var driverOpts map[string]string
			if netConfig != nil {
				aliases = netConfig.Aliases
				driverOpts = netConfig.DriverOpts
			}
			if container.NetworkMode(target).IsUserDefined() {
				aliases = append(aliases, svc.Name)
			}

			networkAttachments = append(networkAttachments, swarm.NetworkAttachmentConfig{
				Target:     target,
				Aliases:    aliases,
				DriverOpts: driverOpts,
			})
		}
	}

	sort.Slice(networkAttachments, func(i, j int) bool {
		return networkAttachments[i].Target < networkAttachments[j].Target
	})
	sort.Slice(containerSpec.Secrets, func(i, j int) bool {
		return containerSpec.Secrets[i].SecretName < containerSpec.Secrets[j].SecretName
	})
	sort.Slice(containerSpec.Configs, func(i, j int) bool {
		return containerSpec.Configs[i].ConfigName < containerSpec.Configs[j].ConfigName
	})

	var mode swarm.ServiceMode
	var endpointMode string
	if svc.Deploy != nil {
		var err error
		mode, err = convertDeployMode(svc.Deploy.Mode, svc.Deploy.Replicas)
		if err != nil {
			return swarm.ServiceSpec{}, err
		}
		endpointMode = svc.Deploy.EndpointMode
	} else {
		mode = swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{},
		}
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   ScopeName(stack, svc.Name),
			Labels: serviceLabels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: containerSpec,
			LogDriver:     convertLogDriver(svc.Logging),
			Networks:      networkAttachments,
		},
		Mode: mode,
	}

	var restartPolicy *swarm.RestartPolicy
	if svc.Deploy != nil {
		var err error
		restartPolicy, err = convertRestartPolicy(svc.Restart, svc.Deploy.RestartPolicy)
		if err != nil {
			return swarm.ServiceSpec{}, err
		}
		spec.TaskTemplate.Resources = convertResources(&svc.Deploy.Resources)
		spec.UpdateConfig = convertUpdateConfig(svc.Deploy.UpdateConfig)
		spec.RollbackConfig = convertUpdateConfig(svc.Deploy.RollbackConfig)
		spec.TaskTemplate.Placement = &swarm.Placement{
			Constraints: svc.Deploy.Placement.Constraints,
			Preferences: convertPlacementPreferences(svc.Deploy.Placement.Preferences),
			MaxReplicas: svc.Deploy.Placement.MaxReplicas,
		}
	} else {
		restartPolicy, _ = convertRestartPolicy(svc.Restart, nil)
	}
	spec.TaskTemplate.RestartPolicy = restartPolicy

	if len(svc.Ports) > 0 || endpointMode != "" {
		portConfigs := make([]swarm.PortConfig, 0, len(svc.Ports))
		for _, port := range svc.Ports {
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
				PublishMode:   swarm.PortConfigPublishMode(port.Mode),
			}
			portConfigs = append(portConfigs, portConfig)
		}

		sort.Slice(portConfigs, func(i, j int) bool {
			if portConfigs[i].PublishedPort != portConfigs[j].PublishedPort {
				return portConfigs[i].PublishedPort < portConfigs[j].PublishedPort
			}
			return portConfigs[i].TargetPort < portConfigs[j].TargetPort
		})

		spec.EndpointSpec = &swarm.EndpointSpec{
			Mode:  swarm.ResolutionMode(strings.ToLower(endpointMode)),
			Ports: portConfigs,
		}
	}

	return spec, nil
}

func convertHealthcheck(healthcheck *types.HealthCheckConfig) (*container.HealthConfig, error) {
	if healthcheck == nil {
		return nil, nil
	}

	if healthcheck.Disable {
		if len(healthcheck.Test) != 0 {
			return nil, errors.New("test and disable can't be set at the same time")
		}
		return &container.HealthConfig{
			Test: []string{"NONE"},
		}, nil
	}

	var timeout, interval, startPeriod, startInterval time.Duration
	var retries int

	if healthcheck.Timeout != nil {
		timeout = time.Duration(*healthcheck.Timeout)
	}
	if healthcheck.Interval != nil {
		interval = time.Duration(*healthcheck.Interval)
	}
	if healthcheck.StartPeriod != nil {
		startPeriod = time.Duration(*healthcheck.StartPeriod)
	}
	if healthcheck.StartInterval != nil {
		startInterval = time.Duration(*healthcheck.StartInterval)
	}
	if healthcheck.Retries != nil {
		retries = int(*healthcheck.Retries)
	}

	return &container.HealthConfig{
		Test:          healthcheck.Test,
		Timeout:       timeout,
		Interval:      interval,
		Retries:       retries,
		StartPeriod:   startPeriod,
		StartInterval: startInterval,
	}, nil
}

func convertResources(source *types.Resources) *swarm.ResourceRequirements {
	if source == nil {
		return nil
	}

	resources := &swarm.ResourceRequirements{}

	if source.Limits != nil {
		resources.Limits = &swarm.Limit{
			NanoCPUs:    int64(source.Limits.NanoCPUs * 1e9),
			MemoryBytes: int64(source.Limits.MemoryBytes),
			Pids:        source.Limits.Pids,
		}
	}

	if source.Reservations != nil {
		var generic []swarm.GenericResource
		for _, res := range source.Reservations.GenericResources {
			var r swarm.GenericResource
			if res.DiscreteResourceSpec != nil {
				r.DiscreteResourceSpec = &swarm.DiscreteGenericResource{
					Kind:  res.DiscreteResourceSpec.Kind,
					Value: res.DiscreteResourceSpec.Value,
				}
			}
			generic = append(generic, r)
		}

		resources.Reservations = &swarm.Resources{
			NanoCPUs:         int64(source.Reservations.NanoCPUs * 1e9),
			MemoryBytes:      int64(source.Reservations.MemoryBytes),
			GenericResources: generic,
		}
	}

	return resources
}

func convertDNSConfig(dns, dnsSearch []string) *swarm.DNSConfig {
	if len(dns) == 0 && len(dnsSearch) == 0 {
		return nil
	}

	return &swarm.DNSConfig{
		Nameservers: toNetIPAddrs(dns),
		Search:      dnsSearch,
	}
}

func toNetIPAddrs(ips []string) []netip.Addr {
	if len(ips) == 0 {
		return nil
	}

	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

func convertUlimits(ulimits map[string]*types.UlimitsConfig) []*container.Ulimit {
	if len(ulimits) == 0 {
		return nil
	}

	result := make([]*container.Ulimit, 0, len(ulimits))
	for name, u := range ulimits {
		if u.Single != 0 {
			result = append(result, &container.Ulimit{
				Name: name,
				Soft: int64(u.Single),
				Hard: int64(u.Single),
			})
		} else {
			result = append(result, &container.Ulimit{
				Name: name,
				Soft: int64(u.Soft),
				Hard: int64(u.Hard),
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func convertExtraHosts(extraHosts types.HostsList) []string {
	var hosts []string
	for hostname, ips := range extraHosts {
		for _, ip := range ips {
			hosts = append(hosts, ip+" "+hostname)
		}
	}
	return hosts
}

func convertDeployMode(mode string, replicas *int) (swarm.ServiceMode, error) {
	serviceMode := swarm.ServiceMode{}

	switch mode {
	case "global-job":
		if replicas != nil {
			return serviceMode, errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		serviceMode.GlobalJob = &swarm.GlobalJob{}
	case "global":
		if replicas != nil {
			return serviceMode, errors.New("replicas can only be used with replicated or replicated-job mode")
		}
		serviceMode.Global = &swarm.GlobalService{}
	case "replicated-job":
		var r *uint64
		if replicas != nil {
			rr := uint64(*replicas)
			r = &rr
		}
		serviceMode.ReplicatedJob = &swarm.ReplicatedJob{
			MaxConcurrent:    r,
			TotalCompletions: r,
		}
	case "replicated", "":
		var r *uint64
		if replicas != nil {
			rr := uint64(*replicas)
			r = &rr
		}
		serviceMode.Replicated = &swarm.ReplicatedService{Replicas: r}
	default:
		return serviceMode, fmt.Errorf("unknown mode: %s", mode)
	}
	return serviceMode, nil
}

func convertRestartPolicy(restart string, source *types.RestartPolicy) (*swarm.RestartPolicy, error) {
	if source == nil {
		if restart == "" || restart == "no" {
			return nil, nil
		}

		name, maxRetries, _ := strings.Cut(restart, ":")
		var maxAttempts *uint64
		if maxRetries != "" {
			count, err := strconv.ParseUint(maxRetries, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid restart policy: %s", restart)
			}
			maxAttempts = &count
		}

		switch name {
		case "always", "unless-stopped":
			return &swarm.RestartPolicy{
				Condition: swarm.RestartPolicyConditionAny,
			}, nil
		case "on-failure":
			return &swarm.RestartPolicy{
				Condition:   swarm.RestartPolicyConditionOnFailure,
				MaxAttempts: maxAttempts,
			}, nil
		default:
			return nil, fmt.Errorf("unknown restart policy: %s", restart)
		}
	}

	var delay, window *time.Duration
	if source.Delay != nil {
		d := time.Duration(*source.Delay)
		delay = &d
	}
	if source.Window != nil {
		w := time.Duration(*source.Window)
		window = &w
	}

	return &swarm.RestartPolicy{
		Condition:   swarm.RestartPolicyCondition(source.Condition),
		Delay:       delay,
		MaxAttempts: source.MaxAttempts,
		Window:      window,
	}, nil
}

func convertUpdateConfig(source *types.UpdateConfig) *swarm.UpdateConfig {
	if source == nil {
		return nil
	}

	parallel := uint64(1)
	if source.Parallelism != nil {
		parallel = *source.Parallelism
	}

	return &swarm.UpdateConfig{
		Parallelism:     parallel,
		Delay:           time.Duration(source.Delay),
		FailureAction:   swarm.FailureAction(source.FailureAction),
		Monitor:         time.Duration(source.Monitor),
		MaxFailureRatio: source.MaxFailureRatio,
		Order:           swarm.UpdateOrder(source.Order),
	}
}

func convertPlacementPreferences(prefs []types.PlacementPreferences) []swarm.PlacementPreference {
	result := make([]swarm.PlacementPreference, 0, len(prefs))
	for _, pref := range prefs {
		result = append(result, swarm.PlacementPreference{
			Spread: &swarm.SpreadOver{
				SpreadDescriptor: pref.Spread,
			},
		})
	}
	return result
}

func convertLogDriver(logging *types.LoggingConfig) *swarm.Driver {
	if logging == nil {
		return nil
	}
	return &swarm.Driver{
		Name:    logging.Driver,
		Options: logging.Options,
	}
}

func lookupSecretID(ctx context.Context, apiClient client.APIClient, name string) (string, error) {
	res, err := apiClient.SecretInspect(ctx, name, client.SecretInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("secret not found: %w", err)
	}
	return res.Secret.ID, nil
}

func lookupConfigID(ctx context.Context, apiClient client.APIClient, name string) (string, error) {
	res, err := apiClient.ConfigInspect(ctx, name, client.ConfigInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("config not found: %w", err)
	}
	return res.Config.ID, nil
}

func effectiveCapAddCapDrop(add, drop []string) (capAdd, capDrop []string) {
	addCaps := capabilitiesMap(add)
	dropCaps := capabilitiesMap(drop)

	if addCaps["ALL"] {
		addCaps = map[string]bool{"ALL": true}
	}
	if dropCaps["ALL"] {
		dropCaps = map[string]bool{"ALL": true}
	}

	for c := range dropCaps {
		if !addCaps[c] {
			capDrop = append(capDrop, c)
		}
	}
	for c := range addCaps {
		capAdd = append(capAdd, c)
	}

	sort.Strings(capAdd)
	sort.Strings(capDrop)
	return capAdd, capDrop
}

func capabilitiesMap(caps []string) map[string]bool {
	normalized := make(map[string]bool)
	for _, c := range caps {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "ALL" && !strings.HasPrefix(c, "CAP_") {
			c = "CAP_" + c
		}
		normalized[c] = true
	}
	return normalized
}

func AddStackLabel(stack string, labels types.Labels) map[string]string {
	result := make(map[string]string)
	maps.Copy(result, labels)
	result[LabelNamespace] = stack
	return result
}

func convertVolumeToMount(vol types.ServiceVolumeConfig, volumes types.Volumes, stack string) (mount.Mount, error) {
	m := mount.Mount{
		Type:        mount.Type(vol.Type),
		Target:      vol.Target,
		ReadOnly:    vol.ReadOnly,
		Source:      vol.Source,
		Consistency: mount.Consistency(vol.Consistency),
	}

	switch vol.Type {
	case "bind":
		if vol.Bind != nil {
			m.BindOptions = &mount.BindOptions{
				Propagation: mount.Propagation(vol.Bind.Propagation),
			}
		}
		return m, nil
	case "tmpfs":
		if vol.Tmpfs != nil {
			m.TmpfsOptions = &mount.TmpfsOptions{
				SizeBytes: int64(vol.Tmpfs.Size),
			}
		}
		return m, nil
	case "npipe":
		if vol.Bind != nil {
			m.BindOptions = &mount.BindOptions{
				Propagation: mount.Propagation(vol.Bind.Propagation),
			}
		}
		return m, nil
	case "image":
		if vol.Image != nil {
			m.ImageOptions = &mount.ImageOptions{
				Subpath: vol.Image.SubPath,
			}
		}
		return m, nil
	case "cluster":
		return handleClusterVolume(vol, volumes, stack)
	case "volume", "":
		// Handle named volumes below
	default:
		return mount.Mount{}, fmt.Errorf("unsupported volume type: %s", vol.Type)
	}

	// Anonymous volumes
	if vol.Source == "" {
		return m, nil
	}

	stackVolume, exists := volumes[vol.Source]
	if !exists {
		return mount.Mount{}, fmt.Errorf("undefined volume %q", vol.Source)
	}

	m.Source = ScopeName(stack, vol.Source)
	m.VolumeOptions = &mount.VolumeOptions{}

	if vol.Volume != nil {
		m.VolumeOptions.NoCopy = vol.Volume.NoCopy
		m.VolumeOptions.Subpath = vol.Volume.Subpath
	}

	if stackVolume.Name != "" {
		m.Source = stackVolume.Name
	}

	// External named volumes
	if bool(stackVolume.External) {
		return m, nil
	}

	m.VolumeOptions.Labels = AddStackLabel(stack, stackVolume.Labels)
	if stackVolume.Driver != "" || stackVolume.DriverOpts != nil {
		m.VolumeOptions.DriverConfig = &mount.Driver{
			Name:    stackVolume.Driver,
			Options: stackVolume.DriverOpts,
		}
	}

	return m, nil
}

func handleClusterVolume(vol types.ServiceVolumeConfig, volumes types.Volumes, stack string) (mount.Mount, error) {
	m := mount.Mount{
		Type:           mount.Type(vol.Type),
		Target:         vol.Target,
		ReadOnly:       vol.ReadOnly,
		Source:         vol.Source,
		ClusterOptions: &mount.ClusterOptions{},
	}

	// Volume groups (prefixed with "group:") are not namespaced
	if strings.HasPrefix(vol.Source, "group:") {
		return m, nil
	}

	stackVolume, exists := volumes[vol.Source]
	if !exists {
		return mount.Mount{}, fmt.Errorf("undefined volume %q", vol.Source)
	}

	if stackVolume.Name != "" {
		m.Source = stackVolume.Name
	} else {
		m.Source = ScopeName(stack, vol.Source)
	}

	return m, nil
}

func convertCredentialSpec(ctx context.Context, apiClient client.APIClient, stack string, spec types.CredentialSpecConfig, configs types.Configs) (*swarm.CredentialSpec, *swarm.ConfigReference, error) {
	if spec.Config == "" && spec.File == "" && spec.Registry == "" {
		return nil, nil, nil
	}

	// Validate only one source is specified
	var sources []string
	if spec.Config != "" {
		sources = append(sources, "Config")
	}
	if spec.File != "" {
		sources = append(sources, "File")
	}
	if spec.Registry != "" {
		sources = append(sources, "Registry")
	}
	if len(sources) > 1 {
		return nil, nil, fmt.Errorf("invalid credential spec: cannot specify both %s", strings.Join(sources, " and "))
	}

	credSpec := &swarm.CredentialSpec{
		File:     spec.File,
		Registry: spec.Registry,
	}

	if spec.Config == "" {
		return credSpec, nil, nil
	}

	config, ok := configs[spec.Config]
	if !ok {
		return nil, nil, fmt.Errorf("credential spec config %q not found", spec.Config)
	}

	configName := ScopeName(stack, spec.Config)
	if config.Name != "" {
		configName = config.Name
	} else if config.External {
		configName = spec.Config
	}

	configID, err := lookupConfigID(ctx, apiClient, configName)
	if err != nil {
		return nil, nil, fmt.Errorf("credential spec config %s: %w", configName, err)
	}

	credSpec.Config = configID

	// Docker CLI adds a Runtime-type config reference for CredentialSpec configs
	configRef := &swarm.ConfigReference{
		ConfigID:   configID,
		ConfigName: configName,
		Runtime:    &swarm.ConfigReferenceRuntimeTarget{},
	}

	return credSpec, configRef, nil
}

