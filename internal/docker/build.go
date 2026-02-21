package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/moby/go-archive"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"github.com/moby/patternmatcher/ignorefile"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type BuildOptions struct {
	Services   map[string]bool
	Cwd        string
	Registries map[string]registry.AuthConfig
	NoCache    bool
	Pull       bool
	Push       bool
	Out        io.Writer
}

func Build(ctx context.Context, dockerClient client.APIClient, project types.Project, opt BuildOptions) error {
	for _, svc := range project.Services {
		if len(opt.Services) > 0 && !opt.Services[svc.Name] {
			continue
		}

		if svc.Build == nil {
			continue
		}

		imageName := svc.Image
		if imageName == "" {
			imageName = project.Name + "_" + svc.Name
		}

		fmt.Fprintf(opt.Out, "Building %s...\n", imageName)

		if err := buildImage(ctx, dockerClient, imageName, svc.Build, svc.Platform, opt); err != nil {
			return fmt.Errorf("failed to build %s: %w", svc.Name, err)
		}

		if opt.Push {
			fmt.Fprintf(opt.Out, "Pushing %s...\n", imageName)
			if err := PushImage(ctx, dockerClient, imageName, opt.Registries); err != nil {
				return fmt.Errorf("failed to push %s: %w", svc.Name, err)
			}
		}
	}

	return nil
}

func readIgnorePatterns(buildContext string) []string {
	f, err := os.Open(filepath.Join(buildContext, ".dockerignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	patterns, _ := ignorefile.ReadAll(f)
	return patterns
}

func buildImage(ctx context.Context, dockerClient client.APIClient, imageName string, build *types.BuildConfig, platform string, opt BuildOptions) error {
	buildContext := build.Context
	if buildContext == "" {
		buildContext = "."
	}

	if !filepath.IsAbs(buildContext) {
		buildContext = filepath.Join(opt.Cwd, buildContext)
	}

	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	excludePatterns := readIgnorePatterns(buildContext)

	buildContextReader, err := archive.TarWithOptions(buildContext, &archive.TarOptions{
		ExcludePatterns: excludePatterns,
	})
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}
	defer buildContextReader.Close()

	tags := []string{imageName}
	tags = append(tags, build.Tags...)

	opts := client.ImageBuildOptions{
		Tags:        tags,
		Dockerfile:  dockerfile,
		BuildArgs:   build.Args,
		NoCache:     opt.NoCache || build.NoCache,
		PullParent:  opt.Pull || build.Pull,
		Remove:      true,
		Target:      build.Target,
		Labels:      build.Labels,
		CacheFrom:   build.CacheFrom,
		NetworkMode: build.Network,
		ShmSize:     int64(build.ShmSize),
	}

	if len(build.ExtraHosts) > 0 {
		opts.ExtraHosts = build.ExtraHosts.AsList(":")
	}

	if build.Isolation != "" {
		opts.Isolation = container.Isolation(build.Isolation)
	}

	if len(build.Ulimits) > 0 {
		opts.Ulimits = make([]*container.Ulimit, 0, len(build.Ulimits))
		for name, u := range build.Ulimits {
			opts.Ulimits = append(opts.Ulimits, &container.Ulimit{
				Name: name,
				Soft: int64(u.Soft),
				Hard: int64(u.Hard),
			})
		}
	}

	if len(build.Platforms) > 0 {
		opts.Platforms = make([]ocispec.Platform, 0, len(build.Platforms))
		for _, ps := range build.Platforms {
			p, err := platforms.Parse(ps)
			if err != nil {
				return fmt.Errorf("invalid platform %q: %w", ps, err)
			}
			opts.Platforms = append(opts.Platforms, p)
		}
	} else if platform != "" {
		p, err := platforms.Parse(platform)
		if err != nil {
			return fmt.Errorf("invalid platform %q: %w", platform, err)
		}
		opts.Platforms = []ocispec.Platform{p}
	}

	resp, err := dockerClient.ImageBuild(ctx, buildContextReader, opts)
	if err != nil {
		return fmt.Errorf("failed to start build: %w", err)
	}
	defer resp.Body.Close()

	return jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, os.Stdout.Fd(), true, nil)
}

func PushImage(ctx context.Context, dockerClient client.APIClient, imageName string, registries map[string]registry.AuthConfig) error {
	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return err
	}

	registryHost := reference.Domain(ref)

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
