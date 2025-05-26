# cicdez - Local CI/CD Tool

A simple, lightweight CI/CD tool for automating pre-build tasks, Docker image building, and deployment workflows locally or in your development environment.

## Why cicdez?

**Faster than CI runners**: Your local machine is probably more powerful than standard GitHub runners, and there's no network latency or cold start delays.

**Clean builds**: Automatically clones to a temporary directory, ensuring your builds match exactly what's in git - no accidental inclusion of uncommitted changes that cause "works locally but fails in production" issues.

**Simple but structured**: More organized than bash scripts, less complex than enterprise CI/CD platforms.

## Features

- **Fast local execution**: Run on your powerful dev machine instead of waiting for slow CI runners
- **Clean git-based builds**: Always builds from a clean git clone, preventing "dirty" builds with uncommitted changes
- **Docker-focused workflow**: Build and push images with automatic git-based tagging
- **Simple configuration**: YAML config that's much simpler than GitHub Actions
- **Secret management**: Integrated SOPS support for encrypted secrets
- **Modular execution**: Run complete pipeline or individual phases (pre/build/deploy)

## Installation

1. Ensure you have Go installed 
2. Clone and build the tool:
```bash
git clone <repository-url>
cd cicdez
go install
```

## Usage

### Run Complete Pipeline
Execute all phases (pre → build → deploy) for a thing:
```bash
cicdez my-app
```

### Run Individual Phases
Execute specific phases:
```bash
# Run only pre-build tasks
cicdez my-app pre

# Run only build phase
cicdez my-app build

# Run only deploy phase
cicdez my-app deploy
```

## Configuration Reference

### Thing Configuration

Each "thing" in your configuration represents a complete CI/CD pipeline with three optional phases:

#### Pre Phase
```yaml
pre:
  - "command arg1 arg2"
  - "another-command"
```
- List of shell commands to execute before building
- Commands run in the temporary git clone directory

#### Build Phase
```yaml
build:
  image: "registry/image-name"     # Required: Docker image name
  file: "Dockerfile"              # Optional: Dockerfile path (default: Dockerfile)
  context: "."                    # Optional: Build context (default: .)
  platform: "linux/amd64"        # Optional: Target platform
  tags:                          # Optional: Image tags
    - "latest"
    - "git_tag"                  # Special: Uses git tag
    - "git_sha"                  # Special: Uses git commit SHA
    - "v1.0.0"                   # Custom tags
```

**Special Tags:**
- `git_tag`: Automatically resolves to the latest git tag using `git describe --tags --abbrev=0`
- `git_sha`: Automatically resolves to the short commit SHA using `git rev-parse --short HEAD`

#### Deploy Phase
```yaml
deploy:
  name: "stack-name"             # Required: Docker stack name
  file: "docker-compose.yml"     # Optional: Compose file path
  context: "production"          # Optional: Docker context
  auth: "true"                   # Optional: Use registry auth
  env:                          # Optional: Environment variables
    KEY: "value"
```

## Secret Management

CICDEZ integrates with [SOPS](https://github.com/mozilla/sops) for encrypted secret management:

1. Create an encrypted secrets file using SOPS:
```bash
sops secrets.yaml
```

2. Set the `SECRET_FILE` environment variable:
```bash
export SECRET_FILE=secrets.yaml
./cicdez my-app
```

Secrets from the file will be automatically loaded as environment variables during execution.

## How It Works

1. **Git Clone**: Creates a temporary directory and clones the current branch
2. **Secret Loading**: If `SECRET_FILE` is set, decrypts and loads secrets as environment variables
3. **Phase Execution**: Runs the requested phases in order:
   - **Pre**: Executes shell commands in the cloned directory
   - **Build**: Builds Docker image with specified tags and pushes to registry
   - **Deploy**: Deploys Docker stack with specified configuration

## Requirements

- **Git**: For repository operations and tagging
- **Docker**: For building images and deploying stacks
- **Docker Swarm**: For stack deployments (if using deploy phase)
- **SOPS** (optional): For encrypted secrets management

## Examples

### Simple Web Application
```yaml
things:
  webapp:
    pre:
      - "npm ci"
      - "npm run build"
    build:
      image: "myregistry/webapp"
      tags: ["latest", "git_sha"]
    deploy:
      name: "webapp-stack"
      file: "docker-compose.prod.yml"
```

### Microservice with Tests
```yaml
things:
  api-service:
    pre:
      - "go mod download"
      - "go test -v ./..."
      - "go build -o app ."
    build:
      image: "myregistry/api-service"
      file: "Dockerfile.prod"
      platform: "linux/amd64,linux/arm64"
      tags: ["latest", "git_tag"]
    deploy:
      name: "api-service"
      context: "production"
      auth: "true"
      env:
        LOG_LEVEL: "info"
        PORT: "8080"
```

## Error Handling

- Configuration file must exist as `cicdez.yaml`
- Thing name must exist in configuration
- Git repository must be initialized
- Docker must be available and running
- All shell commands in pre phase must exit with code 0

## Contributing

This tool is designed to be simple and focused. For feature requests or bug reports, please create an issue in the repository.
