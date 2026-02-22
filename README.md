# cicdez

## Overview

Simple CLI to manage deployments, configuration, and secrets. Everything stays encrypted in your repo. Build, push, and deploy with one command.

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
cicdez server add prod --host example.com --user deploy
cicdez registry add ghcr.io --username user --password token
cicdez secret add DB_PASSWORD
cicdez deploy
```

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
