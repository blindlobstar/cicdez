package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	clitypes "github.com/docker/cli/cli/config/types"
	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"
)

func hasBuildKitSupport(ctx context.Context, dockerClient client.APIClient) bool {
	ping, err := dockerClient.Ping(ctx, client.PingOptions{})
	if err != nil {
		return false
	}
	return ping.BuilderVersion == build.BuilderBuildKit
}

func newBuildKitClient(ctx context.Context, dockerClient client.APIClient) (*bkclient.Client, error) {
	return bkclient.New(ctx, "",
		bkclient.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dockerClient.DialHijack(ctx, "/grpc", "h2c", nil)
		}),
		bkclient.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return dockerClient.DialHijack(ctx, "/session", proto, meta)
		}),
	)
}

func newAuthProvider(registries map[string]registry.AuthConfig) session.Attachable {
	return authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		AuthConfigProvider: func(ctx context.Context, host string, scope []string, _ authprovider.ExpireCachedAuthCheck) (clitypes.AuthConfig, error) {
			if auth, ok := registries[host]; ok {
				return clitypes.AuthConfig{
					Username:      auth.Username,
					Password:      auth.Password,
					Auth:          auth.Auth,
					ServerAddress: auth.ServerAddress,
					IdentityToken: auth.IdentityToken,
					RegistryToken: auth.RegistryToken,
				}, nil
			}
			return clitypes.AuthConfig{}, nil
		},
	})
}

func buildImageWithBuildKit(ctx context.Context, bkClient *bkclient.Client, imageName string, build *types.BuildConfig, projectDir string, opt BuildOptions) error {
	buildContext := build.Context
	if buildContext == "" {
		buildContext = "."
	}
	if !filepath.IsAbs(buildContext) {
		buildContext = filepath.Join(projectDir, buildContext)
	}

	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	if _, err := os.Stat(filepath.Join(buildContext, dockerfile)); err != nil {
		return fmt.Errorf("cannot locate Dockerfile: %s", dockerfile)
	}
	frontendAttrs := map[string]string{
		"filename": dockerfile,
	}

	if build.Target != "" {
		frontendAttrs["target"] = build.Target
	}

	if opt.NoCache || build.NoCache {
		frontendAttrs["no-cache"] = ""
	}

	for k, v := range build.Args {
		if v != nil {
			frontendAttrs["build-arg:"+k] = *v
		}
	}

	for k, v := range build.Labels {
		frontendAttrs["label:"+k] = v
	}

	if len(build.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(build.Platforms, ",")
	}

	tags := []string{imageName}
	tags = append(tags, build.Tags...)

	fs, err := fsutil.NewFS(buildContext)
	if err != nil {
		return err
	}

	solveOpt := bkclient.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts: map[string]fsutil.FS{
			"context":    fs,
			"dockerfile": fs,
		},
		Session: []session.Attachable{
			newAuthProvider(opt.Registries),
		},
		Exports: []bkclient.ExportEntry{{
			Type: "moby",
			Attrs: map[string]string{
				"name": strings.Join(tags, ","),
			},
		}},
	}

	ch := make(chan *bkclient.SolveStatus)
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		_, err := bkClient.Solve(ctx, nil, solveOpt, ch)
		return err
	})

	eg.Go(func() error {
		display, err := progressui.NewDisplay(opt.Out, progressui.AutoMode)
		if err != nil {
			return err
		}
		_, err = display.UpdateFrom(ctx, ch)
		return err
	})

	return eg.Wait()
}
