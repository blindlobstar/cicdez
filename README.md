# cicdez

Easy deployment and continuous delivery tool using Docker Swarm and age encryption.

## Table of Contents

- [Overview](#overview)
- [Getting Started](#getting-started)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
  - [Directory Structure](#directory-structure)
  - [Global Configuration & Encryption](#global-configuration--encryption)
- [CLI Reference](#cli-reference)
  - [server](#server)
  - [secret](#secret)
  - [registry](#registry)
  - [build](#build)
  - [deploy](#deploy)
- [Docker Compose Reference](#docker-compose-reference)
  - [Git Context Variables](#git-context-variables)
  - [Configs](#configs)
  - [Prebuild](#prebuild)
  - [Secrets](#secrets-1)
- [Examples](#examples)
  - [Complete Docker Compose](#complete-docker-compose)

---

## Overview

cicdez simplifies deployment management by:
- Managing secrets with age encryption
- Extending Docker Compose with git context and custom features
- Deploying to Docker Swarm with version control
- Tracking configuration changes via git

## Getting Started

### Installation

[TBD]

### Quick Start

1. Generate an age key and set the environment variable:
   ```bash
   age-keygen -o ~/.config/cicdez/age.key
   export CICDEZ_AGE_KEY_FILE=~/.config/cicdez/age.key
   ```

2. Add a server:
   ```bash
   cicdez server add production --host 1.2.3.4 --user deploy --key ~/.ssh/id_rsa
   ```

3. Add secrets:
   ```bash
   cicdez secret add db_password mypassword
   cicdez secret add api_key sk-1234567890
   ```

4. Deploy:
   ```bash
   cicdez deploy -c docker-compose.yml --server production
   ```

## Core Concepts

### Directory Structure

```
.cicdez/
├── config.age              # encrypted project config (servers, registries)
└── secrets.age             # encrypted secrets
```

### Global Configuration & Encryption

cicdez uses **age** encryption to protect sensitive data.

**Encryption key setup:**

Before using cicdez, generate an age key and set the environment variable:
```bash
age-keygen -o ~/.config/cicdez/age.key
export CICDEZ_AGE_KEY_FILE=~/.config/cicdez/age.key
```

Add the export to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to make it permanent.

**What is encrypted:**
- `.cicdez/config.age` - server credentials, registry auth
- `.cicdez/secrets.age` - application secrets

**How it works:**
1. You generate an age key using `age-keygen`
2. Set `CICDEZ_AGE_KEY_FILE` to point to your key
3. When adding sensitive data (servers, secrets, registries), cicdez encrypts with age
4. Encrypted files are safe to commit to git
5. At deploy time, cicdez automatically decrypts using your age key
6. Secrets are deployed to Docker Swarm as Docker secrets

**CI/CD usage:**

```bash
# Set environment variable to the key location
export CICDEZ_AGE_KEY_FILE=/tmp/ci-age-key
echo "$CI_AGE_KEY_SECRET" > /tmp/ci-age-key
cicdez deploy -c docker-compose.yml --server production
```

---

## CLI Reference

### server

Manage servers for deployment.

**Commands:**

#### `add <name> <host>`
Add an already-configured server to the project.

Use this command when the server already has a user with Docker access configured.

**Usage:**
```bash
cicdez server add <name> <host> --user <user> --key <path>
```

**Arguments:**
- `name` (required) - Server identifier (e.g., production, staging)
- `host` (required) - IP address or hostname of the server

**Options:**
- `--user <string>` (required) - SSH username for deployment
- `--key <path>` (required) - Path to SSH private key

**Example:**
```bash
cicdez server add production 192.168.1.100 --user deployer --key ~/.ssh/deployer_key
```

**Notes:**
- The user must already exist on the server
- Docker and Docker Swarm must already be installed and configured
- The private key will be stored encrypted in `.cicdez/config.age`

#### `init <name> <host>`
Initialize and configure a new server from scratch.

Use this command to set up a fresh server with deployer user, Docker, and Swarm.

**Usage:**
```bash
cicdez server init <name> <host> [options]
```

**Arguments:**
- `name` (required) - Server identifier (e.g., production, staging)
- `host` (required) - IP address or hostname of the server

**Options:**
- `--root-key <path>` (optional) - Path to existing SSH private key for root access
- `--disable-password-auth` (optional) - Disable password authentication after setup

**What it does:**

This command initializes a server for cicdez deployments by:

1. **Root SSH Key Setup**
   - **If `--root-key` is provided**: Uses existing key for root access
   - **If `--root-key` is NOT provided**:
     - Generates a new SSH key pair for root
     - Prompts for root password to copy public key to server
     - Installs public key to root's `authorized_keys`
   - Stores root private key in `~/.ssh/cicdez_<server-name>_root`
   - Sets proper SSH permissions (600)

2. **Deployer User Creation**
   - Creates a new user named `deployer` on the remote server
   - Generates a new SSH key pair for the deployer user
   - Configures deployer's SSH `authorized_keys`
   - Adds deployer to `docker` group for Docker access
   - Stores deployer's private key encrypted in `.cicdez/config.age`

3. **Docker Installation**
   - Installs Docker Engine on the server (if not already installed)
   - Enables and starts Docker service
   - Configures Docker to start on boot

4. **Docker Swarm Setup**
   - Initializes Docker Swarm mode on the server
   - Stores Swarm join tokens (encrypted) for adding more nodes later

5. **Security Configuration** (if `--disable-password-auth` is used)
   - Disables password authentication in SSH config
   - Requires key-based authentication only
   - Restarts SSH service

6. **Server Registration**
   - Automatically adds the server to project configuration (same as `server add`)

**Examples:**

Using existing root key:
```bash
cicdez server init production 192.168.1.100 --root-key ~/.ssh/id_rsa
```

Generate new root key (will prompt for password):
```bash
cicdez server init production 192.168.1.100
# You will be prompted: "Enter root password for 192.168.1.100:"
```

With password authentication disabled:
```bash
cicdez server init production 192.168.1.100 --disable-password-auth
```

**Server Architecture After Initialization:**

```
Remote Server
├── root user
│   ├── SSH key: ~/.ssh/cicdez_production_root (local machine)
│   └── Password auth: enabled (or disabled with --disable-password-auth)
├── deployer user
│   ├── SSH key: stored encrypted in .cicdez/config.age
│   ├── Member of: docker group
│   └── Permissions: Docker management
└── Docker Swarm
    └── Mode: manager (single-node swarm)
```

**Notes:**
- Root access is only needed during initialization
- All subsequent deployments use the `deployer` user
- Generated root keys are saved locally and can be used for future server administration
- The deployer user has Docker access via group membership (no sudo needed)
- Swarm initialization uses the server's primary IP address

### secret

Manage sensitive values (passwords, tokens, API keys, etc.).

Secrets are stored encrypted in `.cicdez/secrets.age` using age encryption. Secrets can be referenced in docker-compose files and are deployed as Docker Swarm secrets.

**Commands:**

- `add <name> <value>` - Add new secret
- `list` / `ls` - List all secret names (not values)
- `edit` - Edit all secrets with your `$EDITOR`
- `remove <name>` - Remove secret

**Examples:**
```bash
cicdez secret add db_password supersecret123
cicdez secret list
cicdez secret edit
cicdez secret remove old_api_key
```

### registry

Manage Docker registry authentication for pushing and pulling images.

Registry credentials are stored encrypted in `.cicdez/config.age`.

**Commands:**

- `add` - Add new registry with authentication
  - `--url` (string) - Registry URL (e.g., docker.io, gcr.io, ghcr.io)
  - `--username` (string) - Registry username
  - `--password` (string) - Registry password or token
- `list` / `ls` - List all configured registries
- `remove <url>` / `rm` - Remove registry by URL

**Examples:**
```bash
cicdez registry add --url ghcr.io --username myuser --password ghp_token123
cicdez registry list
cicdez registry remove ghcr.io
```

### build

Build service images from a docker-compose file.

**Usage:**
```bash
cicdez build [service...] -c <compose-file> [--env-file <path>]
```

**Arguments:**
- `service` (optional) - One or more service names to build. If omitted, builds all services with `build` sections.

**Options:**
- `-c` / `--compose-file` (required) - Path to docker-compose file
- `--env-file` (optional) - Path to environment file. Defaults to `.env` in current directory

**Examples:**

Build all services:
```bash
cicdez build -c docker-compose.yml
```

Build specific service:
```bash
cicdez build web -c docker-compose.yml
```

Build multiple services:
```bash
cicdez build web api -c docker-compose.yml
```

With custom env file:
```bash
cicdez build -c docker-compose.yml --env-file .env.production
```

**What it does:**
- Runs prebuild jobs if defined
- Builds Docker images for specified services
- Tags images according to docker-compose configuration
- Substitutes git context variables

### deploy

Deploy services to Docker Swarm from a docker-compose file.

**Usage:**
```bash
cicdez deploy [service...] -c <compose-file> --server <server> [--stack <name>] [--env-file <path>]
```

**Arguments:**
- `service` (optional) - One or more service names to deploy. If omitted, deploys all services.

**Options:**
- `-c` / `--compose-file` (required) - Path to docker-compose file
- `--server` (required) - Target server name
- `--stack` (optional) - Stack name for Docker Swarm. If omitted, auto-detects from:
  1. `name` field in docker-compose.yml
  2. Git repository name
  3. Current directory name
- `--env-file` (optional) - Path to environment file. Defaults to `.env` in current directory

**Examples:**

Deploy all services:
```bash
cicdez deploy -c docker-compose.yml --server production
```

Deploy specific service:
```bash
cicdez deploy web -c docker-compose.yml --server production
```

Deploy multiple services:
```bash
cicdez deploy web db -c docker-compose.yml --server production
```

Deploy with explicit stack name:
```bash
cicdez deploy -c docker-compose.yml --server production --stack myapp
```

Deploy with custom env file:
```bash
cicdez deploy -c docker-compose.yml --server production --env-file .env.production
```

**What it does:**
- Processes docker-compose file with variable substitution
- Creates/updates Docker Swarm secrets from `.cicdez/secrets.age`
- Creates/updates Docker Swarm configs (for `local: true` configs)
- Deploys services to the target server's Docker Swarm
- Uses `docker stack deploy` under the hood

---

## Docker Compose Reference

cicdez extends standard Docker Compose with custom syntax for easier deployment management.

### Git Context Variables

cicdez provides git context variables that can be used anywhere in docker-compose files using `{git.property}` syntax.

**Available Variables:**

- **`{git.sha}`** - Full commit SHA
  - Example: `a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0`

- **`{git.short_sha}`** - Short commit SHA (7 characters)
  - Example: `a1b2c3d`

- **`{git.tag}`** - Current git tag (if exists)
  - Example: `v1.2.3`

- **`{git.branch}`** - Current branch name
  - Example: `main`, `develop`

- **`{git.author}`** - Commit author name
  - Example: `John Doe`

- **`{git.author_email}`** - Commit author email
  - Example: `john@example.com`

- **`{git.message}`** - Commit message
  - Example: `fix: resolve bug`

**Usage:**

Git context variables are replaced at build and deploy time.

**Example:**
```yaml
services:
  web:
    image: myregistry.com/myapp:{git.tag}
    # or use short SHA for dev builds
    # image: myregistry.com/myapp:{git.short_sha}
    environment:
      - GIT_COMMIT={git.sha}
      - GIT_BRANCH={git.branch}
      - DEPLOYED_BY={git.author}
    labels:
      - "git.sha={git.sha}"
      - "git.tag={git.tag}"
      - "git.author={git.author}"
```

### Local Configs

The `local_configs` section allows cicdez to manage local files as versioned Docker configs automatically.

cicdez reads local files (anywhere in your project) and creates versioned Docker configs automatically. The version is based on the file's git commit SHA.

**Syntax:**
```yaml
local_configs:
  - source: <path-to-local-file>
    target: <container-path>
```

**Example:**
```yaml
local_configs:
  - source: ./configs/nginx.conf
    target: /etc/nginx/nginx.conf
  - source: ./app/config.yaml
    target: /app/config.yaml
```

**How it works:**
1. At deploy time, cicdez reads the local file (e.g., `./configs/nginx.conf`)
2. Gets the last commit SHA where this file was modified
3. Checks if a Docker config with name `nginx_conf_<sha>` exists
4. If not, creates a new Docker config with the file contents
5. Mounts the config to the specified target path in the container

**Versioning:** Each time the local file changes (new commit), cicdez creates a new Docker config. Old configs remain until manually removed.

**Note:** For standard Docker configs that already exist in Docker Swarm, use the regular `configs` section with `external: true`.

### Prebuild

The `prebuild` section allows you to run jobs before building the Docker image. Each job runs in a clean copy of the repository and can execute in a container or on the host machine. Common use cases include running tests, linters, type checkers, or any validation that should pass before building.

**Syntax:**

```yaml
services:
  web:
    build:
      context: .
    prebuild:
      - name: <job-name>
        runs-on: <docker-image>  # optional
        commands:
          - name: <command-name>
            command: <command>
```

**Fields:**

- **`name`** (required) - Job name for logging and identification
- **`runs-on`** (optional) - Docker image to run the job in. If omitted, runs on the host machine
- **`commands`** (required) - List of commands to execute in order
  - **`name`** (required) - Command description
  - **`command`** (required) - Shell command to execute

**How it works:**

1. When you run `cicdez build`, prebuild jobs execute first for each service
2. For each job, cicdez creates a clean copy of the repository
3. If `runs-on` is specified, the job runs inside a Docker container with that image
4. If `runs-on` is omitted, the job runs directly on the host machine
5. Commands execute in order within each job
6. Each command must exit with code 0 (success)
7. If any command fails, the build stops immediately
8. If all jobs and commands succeed, the Docker build proceeds

**Examples:**

Run tests in a container:
```yaml
prebuild:
  - name: Test Suite
    runs-on: node:18
    commands:
      - name: Install dependencies
        command: npm ci
      - name: Run tests
        command: npm test
      - name: Type check
        command: npm run type-check
```

Run on host machine:
```yaml
prebuild:
  - name: Local validation
    commands:  # no runs-on = runs on host
      - name: Check formatting
        command: ./scripts/check-format.sh
      - name: Security scan
        command: ./scripts/scan.sh
```

Multiple jobs with different environments:
```yaml
prebuild:
  - name: Backend tests
    runs-on: golang:1.21
    commands:
      - name: Run Go tests
        command: go test ./...
      - name: Run Go vet
        command: go vet ./...

  - name: Frontend tests
    runs-on: node:18
    commands:
      - name: Install dependencies
        command: npm ci
      - name: Run tests
        command: npm test
      - name: Build check
        command: npm run build

  - name: Security scan
    runs-on: alpine:latest
    commands:
      - name: Install trivy
        command: apk add --no-cache curl && curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin
      - name: Scan dependencies
        command: trivy fs .
```

Mixed host and container execution:
```yaml
prebuild:
  - name: Fast local checks
    commands:  # runs on host for speed
      - name: Lint
        command: npm run lint

  - name: Full test suite
    runs-on: ubuntu-22.04  # isolated environment
    commands:
      - name: Install deps
        command: npm ci
      - name: Test
        command: npm test
```

### Secrets

The `sensitive` section is a cicdez-specific extension that manages how secrets are injected into containers. Secrets are grouped by target file.

**Syntax:**
```yaml
sensitive:
  - target: <path>
    format: env|json|raw|template
    secrets:
      - source: <secret-name>
        name: <optional-rename>
    template: <path>  # required for template format
    uid: <user-id>
    gid: <group-id>
    mode: <permissions>
```

#### Format Types

##### `env` (default)
Creates a file with `KEY=VALUE` pairs. Multiple secrets are merged into one file.

```yaml
sensitive:
  - target: /app/.env
    format: env  # optional, this is default
    secrets:
      - source: db_password
        name: DATABASE_PASSWORD  # optional: rename secret
      - source: api_key
        name: API_KEY
    uid: '1000'
    gid: '1000'
    mode: 0440
```

**Output (`/app/.env`):**
```env
DATABASE_PASSWORD=secret_value_1
API_KEY=secret_value_2
```

##### `json`
Creates a JSON file with simple key-value structure.

```yaml
sensitive:
  - target: /app/secrets.json
    format: json
    secrets:
      - source: db_password
        name: database_password
      - source: api_key
```

**Output (`/app/secrets.json`):**
```json
{
  "database_password": "secret_value_1",
  "api_key": "secret_value_2"
}
```

##### `raw`
Writes the secret value directly to a file without any formatting. **Only one secret is allowed** - multiple secrets will cause an error.

```yaml
sensitive:
  - target: /run/secrets/postgres_password
    format: raw
    secrets:
      - db_password
    uid: '999'
    gid: '999'
    mode: 0440
```

**Output (`/run/secrets/postgres_password`):**
```
secret_value
```

##### `template`
Uses a custom template file to generate the output. Secrets are available as variables in the template.

```yaml
sensitive:
  - target: /app/config.yaml
    format: template
    template: ./templates/config.yaml.tmpl
    secrets:
      - source: db_password
        name: db_pass
      - source: api_key
```

**Template file (`./templates/config.yaml.tmpl`):**
```yaml
database:
  password: {{ .db_pass }}
api:
  key: {{ .api_key }}
```

#### Fields Reference

**`target`** (required)
- Path where the secret file will be mounted in the container

**`format`** (optional)
- Output format: `env`, `json`, `raw`, or `template`
- Default: `env`

**`secrets`** (required)
- List of secrets to include
- **`source`** (required) - Secret name from `cicdez secret list`
- **`name`** (optional) - Rename the secret in the output file. If omitted, uses source name

**`template`** (conditional)
- Path to template file
- Required for `template` format

**`uid`** (optional)
- User ID for file ownership

**`gid`** (optional)
- Group ID for file ownership

**`mode`** (optional)
- File permissions (e.g., `0440`)

---

## Examples

### Complete Docker Compose

```yaml
services:
  web:
    image: myregistry.com/myapp:{git.tag}
    build:
      context: .
      dockerfile: .dockerfile
    prebuild:
      - name: Test and Lint
        runs-on: node:18
        commands:
          - name: Install dependencies
            command: npm ci
          - name: Run tests
            command: npm test
          - name: Lint code
            command: npm run lint
    deploy:
      replicas: 3
      update_config:
        parallelism: 1
        delay: 10s
        order: start-first
      rollback_config:
        parallelism: 1
        delay: 5s
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
      placement:
        constraints:
          - node.role == worker
      resources:
        limits:
          cpus: '0.5'
          memory: 512M
        reservations:
          cpus: '0.25'
          memory: 256M
    ports:
      - "8080:80"
    networks:
      - app-network
    environment:
      - NODE_ENV=production
      - API_URL=${API_URL}
      - GIT_COMMIT={git.sha}
    sensitive:
      - target: /app/.sensitive.env
        format: env
        secrets:
          - source: db_password
            name: DB_PASSWORD
          - source: api_key
            name: API_KEY
        uid: '1000'
        gid: '1000'
        mode: 0440
    local_configs:
      - source: ./configs/nginx.conf
        target: /etc/nginx/nginx.conf
    volumes:
      - app-data:/var/www/html
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s

  db:
    image: postgres:15
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.type == database
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: dbuser
    sensitive:
      - target: /run/secrets/postgres_password
        format: raw
        secrets:
          - db_password
        uid: '999'
        gid: '999'
        mode: 0440
    volumes:
      - db-data:/var/lib/postgresql/data
    networks:
      - app-network

networks:
  app-network:
    driver: overlay
    attachable: true

volumes:
  app-data:
  db-data:
```

### Common Workflows

#### Initial Setup

```bash
# Generate age key and set environment variable
age-keygen -o ~/.config/cicdez/age.key
export CICDEZ_AGE_KEY_FILE=~/.config/cicdez/age.key

# Add production server
cicdez server add production --host 192.168.1.100 --user deploy --key ~/.ssh/id_rsa

# Add registry authentication
cicdez registry add --url ghcr.io --username myuser --password ghp_token123

# Add secrets
cicdez secret add db_password supersecret123
cicdez secret add api_key sk-1234567890
```

#### Build and Deploy
```bash
# Build all services
cicdez build -c docker-compose.yml

# Build specific service
cicdez build web -c docker-compose.yml

# Build with custom env file
cicdez build -c docker-compose.yml --env-file .env.production

# Deploy all services
cicdez deploy -c docker-compose.yml --server production

# Deploy specific service
cicdez deploy web -c docker-compose.yml --server production

# Deploy with custom env file
cicdez deploy -c docker-compose.yml --server production --env-file .env.production
```

#### Update Secrets
```bash
# Edit secrets
cicdez secret edit

# Redeploy to apply changes
cicdez deploy -c docker-compose.yml --server production
```
