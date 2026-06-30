package docker

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/blindlobstar/cicdez/internal/vault"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"golang.org/x/sync/errgroup"
)

const RegistrylessPrefix = "registryless/"

func IsRegistryless(image string) bool {
	return strings.HasPrefix(image, RegistrylessPrefix)
}

func pinRef(image, id string) (string, error) {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", err
	}
	tagged, err := reference.WithTag(named, "cicdez-"+strings.TrimPrefix(id, "sha256:")[:12])
	if err != nil {
		return "", err
	}
	return reference.FamiliarString(tagged), nil
}

func PinServices(ctx context.Context, dockerClient client.APIClient, project *types.Project) error {
	for name, svc := range project.Services {
		if !IsRegistryless(svc.Image) {
			continue
		}

		res, err := dockerClient.ImageInspect(ctx, svc.Image)
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", svc.Image, err)
		}

		// the pinned tag rides the same image as the user tag — read it back
		// from RepoTags instead of deriving it.
		named, err := reference.ParseNormalizedNamed(svc.Image)
		if err != nil {
			return err
		}
		prefix := reference.FamiliarName(named) + ":cicdez-"

		pinned := ""
		for _, tag := range res.RepoTags {
			if strings.HasPrefix(tag, prefix) {
				pinned = tag
				break
			}
		}
		if pinned == "" {
			return fmt.Errorf("%s has no cicdez tag on the manager, push it first", svc.Image)
		}

		svc.Image = pinned
		project.Services[name] = svc
	}
	return nil
}

// id seeds the pinned tag name; the build reports it (config digest — the one
// store-independent identity, same value for every daemon holding the artifact)
func PushRegistryless(ctx context.Context, dockerClient client.APIClient, image, id string, servers map[string]vault.Server, out io.Writer) error {
	if id == "" {
		return fmt.Errorf("build did not report an image id for %s", image)
	}

	pinned, err := pinRef(image, id)
	if err != nil {
		return err
	}

	eg, ctx := errgroup.WithContext(ctx)
	for host, server := range servers {
		eg.Go(func() error {
			node, err := NewClientSSH(host, server.Port, server.User, server.Key)
			if err != nil {
				return fmt.Errorf("%s: %w", host, err)
			}
			defer node.Close()

			// pinned tag is content-addressed: tag exists = content exists.
			// still move the user tag, like a registry push that skips
			// every layer but writes the manifest
			if _, err := node.ImageInspect(ctx, pinned); err == nil {
				if _, err := node.ImageTag(ctx, client.ImageTagOptions{Source: pinned, Target: image}); err != nil {
					return fmt.Errorf("%s: %w", host, err)
				}
				fmt.Fprintf(out, "%s: image already exists\n", host)
				return nil
			}

			pr, pw := io.Pipe()
			go func() {
				img, err := dockerClient.ImageSave(ctx, []string{image})
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				defer img.Close()

				gz := gzip.NewWriter(pw)
				if _, err := io.Copy(gz, img); err != nil {
					pw.CloseWithError(err)
					return
				}
				pw.CloseWithError(gz.Close())
			}()

			res, err := node.ImageLoad(ctx, pr)
			if err != nil {
				pr.CloseWithError(err)
				return fmt.Errorf("%s: %w", host, err)
			}
			defer res.Close()

			if err := jsonmessage.DisplayJSONMessagesStream(res, io.Discard, 0, false, nil); err != nil {
				return fmt.Errorf("%s: %w", host, err)
			}

			if _, err := node.ImageTag(ctx, client.ImageTagOptions{Source: image, Target: pinned}); err != nil {
				return fmt.Errorf("%s: %w", host, err)
			}

			fmt.Fprintf(out, "%s: image loaded\n", host)
			return nil
		})
	}
	return eg.Wait()
}
