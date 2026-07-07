package docker

import (
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/moby/moby/api/types/registry"
)

// docker hub credentials live under the legacy index server key,
// not the "docker.io" domain that image references normalize to
const indexServer = "https://index.docker.io/v1/"

func LoadDockerAuth() *configfile.ConfigFile {
	return config.LoadDefaultConfigFile(io.Discard)
}

func resolveAuth(authCfg *configfile.ConfigFile, image string) registry.AuthConfig {
	if authCfg == nil {
		return registry.AuthConfig{}
	}

	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return registry.AuthConfig{}
	}

	key := reference.Domain(ref)
	if key == "docker.io" {
		key = indexServer
	}

	auth, err := authCfg.GetAuthConfig(key)
	if err != nil {
		return registry.AuthConfig{}
	}

	return registry.AuthConfig{
		Username:      auth.Username,
		Password:      auth.Password,
		Auth:          auth.Auth,
		ServerAddress: auth.ServerAddress,
		IdentityToken: auth.IdentityToken,
		RegistryToken: auth.RegistryToken,
	}
}

func allAuthConfigs(authCfg *configfile.ConfigFile) map[string]registry.AuthConfig {
	if authCfg == nil {
		return nil
	}

	creds, err := authCfg.GetAllCredentials()
	if err != nil {
		return nil
	}

	configs := make(map[string]registry.AuthConfig, len(creds))
	for host, auth := range creds {
		configs[host] = registry.AuthConfig{
			Username:      auth.Username,
			Password:      auth.Password,
			Auth:          auth.Auth,
			ServerAddress: auth.ServerAddress,
			IdentityToken: auth.IdentityToken,
			RegistryToken: auth.RegistryToken,
		}
	}
	return configs
}

func encodeAuth(auth registry.AuthConfig) string {
	authBytes, err := json.Marshal(auth)
	if err != nil {
		return ""
	}

	return base64.URLEncoding.EncodeToString(authBytes)
}
