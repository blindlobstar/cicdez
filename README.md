# cicdez

## Overview

Simple CLI to provision servers and  manage deployments, configuration, and secrets. Everything stays encrypted in your repo. Build, push, and deploy with one command.

**Why cicdez?**
- Secrets encrypted with [age](https://github.com/FiloSottile/age) and stored in your repo
- Simple config management with `local_configs`
- No external secret manager or CI/CD platform required
- Single binary, no dependencies
- Uses standard Docker Compose files

**How it works:**
- Define services in `docker-compose.yaml`
- Add servers and registry credentials with `cicdez server add` and `cicdez registry add`
- Add secrets with `cicdez secret add`
- Deploy with `cicdez deploy`

Secrets are decrypted at deploy time and injected as Docker secrets.

## Installation

```bash
go install github.com/blindlobstar/cicdez@latest
```

## Quick Start

```bash
cicdez key generate
cicdez server add example.com --user deploy --setup
cicdez registry add ghcr.io --username user --password token
cicdez secret add DB_PASSWORD
cicdez deploy
```

## Encryption Key

Secrets are encrypted using [age](https://github.com/FiloSottile/age). The key is stored at:

```
~/.config/cicdez/age.key
```

Override with `CICDEZ_AGE_KEY_FILE` environment variable or `--output` flag when generating.

## Server Management

Manage your deployment servers with the following commands:

```bash
# Add a server to the cluster
cicdez server add example.com --user root --setup

# List configured servers
cicdez server list
cicdez server ls

# Remove a server from the cluster
cicdez server remove example.com
cicdez server rm example.com
```

## Server Provisioning

The `--setup` flag provisions a fresh server for deployment. It performs the following steps:

1. **Connects via SSH** - Uses password or key-based authentication
2. **Generates SSH keypairs** - Creates Ed25519 keys for secure access
3. **Creates deployment user** - Sets up a `cicdez` user for deployments
4. **Installs Docker Engine** - Installs Docker and enables the service
5. **Configures Docker Swarm** - Initializes or joins an existing cluster
6. **Optionally hardens SSH** - Disables password authentication with `--disable-password-auth`

Example provisioning a new server:

```bash
# Provision with password auth (will prompt for password)
cicdez server add 192.168.1.100 --user root --setup

# Provision with existing SSH key
cicdez server add 192.168.1.100 --user root --key-file ~/.ssh/id_ed25519 --setup

# Provision and disable password auth
cicdez server add 192.168.1.100 --user root --setup --disable-password-auth
```

## Docker Swarm Cluster

cicdez automatically manages a Docker Swarm cluster across your servers:

- **First server** initializes a new Swarm cluster
- **Additional servers** automatically join the existing cluster
- **Managers vs Workers** - Use `--role worker` to add worker-only nodes

```bash
# Initialize cluster with first manager
cicdez server add manager1.example.com --setup

# Add another manager
cicdez server add manager2.example.com --setup --role manager

# Add a worker node
cicdez server add worker1.example.com --setup --role worker
```

When removing a server, cicdez gracefully drains tasks before removing the node:

```bash
# Gracefully remove from cluster (drains tasks first)
cicdez server remove worker1.example.com

# Remove from config only (doesn't leave swarm)
cicdez server remove worker1.example.com --soft
```

## Secrets Format

Secrets are stored as flat YAML key-value pairs:

```yaml
DB_PASSWORD: secret123
API_KEY: mykey
```

Nested structures are not supported. Use `cicdez secret edit` to modify secrets directly.

## Compose Extensions

### sensitive

Inject encrypted secrets into containers:

```yaml
services:
  app:
    image: myapp
    sensitive:
      app-secrets:
        secrets:
          - source: DB_PASSWORD
          - source: API_KEY
            name: MY_API_KEY
        target: /run/secrets/app.env
        format: env
```

**Formats:**

- `env` (default) - Key=value pairs, one per line
- `json` - JSON object
- `raw` - Single secret value (requires exactly one secret)
- `template` - Render secrets into a [Go template](https://pkg.go.dev/text/template) file

**Template example:**

```yaml
sensitive:
  app-config:
    target: /app/config.yaml
    format: template
    template: ./templates/config.yaml.tmpl
    secrets:
      - source: db_password
        name: db_pass
      - source: api_key
```

Template file (`./templates/config.yaml.tmpl`):
```yaml
database:
  password: {{ .db_pass }}
api:
  key: {{ .api_key }}
```

### local_configs

Mount local files as Docker configs:

```yaml
services:
  nginx:
    image: nginx
    local_configs:
      nginx-conf:
        source: ./nginx.conf
        target: /etc/nginx/nginx.conf
```

Files are hashed so config changes trigger service updates.

## Building

```bash
go build
```

## Testing

```bash
go test ./...
```

## License

MIT
