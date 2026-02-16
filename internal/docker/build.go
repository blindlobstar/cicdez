package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"github.com/moby/go-archive"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"github.com/moby/patternmatcher/ignorefile"
)

type BuildOptions struct {
	Services   map[string]bool
	Cwd        string
	Registries map[string]registry.AuthConfig
	NoCache    bool
	Pull       bool
	Push       bool
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

		fmt.Printf("Building %s...\n", imageName)

		if err := buildImage(ctx, dockerClient, imageName, svc.Build, opt); err != nil {
			return fmt.Errorf("failed to build %s: %w", svc.Name, err)
		}

		if opt.Push {
			fmt.Printf("Pushing %s...\n", imageName)
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

func buildImage(ctx context.Context, dockerClient client.APIClient, imageName string, build *types.BuildConfig, opt BuildOptions) error {
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

	opts := client.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: dockerfile,
		BuildArgs:  build.Args,
		NoCache:    opt.NoCache || build.NoCache,
		PullParent: opt.Pull || build.Pull,
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
