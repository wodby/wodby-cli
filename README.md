# Wodby CLI 2.0

[![Build Status](https://github.com/wodby/wodby-cli/workflows/Build/badge.svg)](https://github.com/wodby/wodby-cli/actions)
[![Docker Pulls](https://img.shields.io/docker/pulls/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Stars](https://img.shields.io/docker/stars/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)

This project provides a unified command line interface to Wodby 2.0

## Install

Fetch the [latest release](https://github.com/wodby/wodby-cli/releases) for your platform:

#### Linux (amd64)

```bash
export WODBY_CLI_LATEST_URL=$(curl -s https://api.github.com/repos/wodby/wodby-cli/releases/latest | grep linux-amd64 | grep browser_download_url | cut -d '"' -f 4)
wget -qO- "${WODBY_CLI_LATEST_URL}" | sudo tar xz -C /usr/local/bin
```

#### macOS

```bash
export WODBY_CLI_LATEST_URL=$(curl -s https://api.github.com/repos/wodby/wodby-cli/releases/latest | grep darwin-amd64 | grep browser_download_url | cut -d '"' -f 4)
wget -qO- "${WODBY_CLI_LATEST_URL}" | tar xz -C /usr/local/bin
```

## Usage

Before using the CLI, set your Wodby API key:

```bash
export WODBY_API_KEY=...
```

After that you can run the CLI with `wodby`.

### Global flags

Commonly used global flags:

```text
--access-token string     Access token
--api-base-url string     Public REST API base URL (default "https://api.wodby.com/v1")
--api-endpoint string     GraphQL API endpoint used by CI commands (default "https://apiv2.wodby.com/query")
--api-key string          API key
--ci-config-path string   Path to CI config (default "/tmp/.wodby-ci.json")
--verbose                 Verbose output
```

### Commands

`wodby` currently exposes:

```text
app         Manage apps, app instances, app services, and app routes
backup      Manage app and database backups
build       Manage app builds
ci          CI-oriented build, release, deploy, and run workflows
completion  Generate shell completion scripts
deployment  Manage deployments
env         Manage environments
help        Help about any command
import      Manage imports
instance    Alias for app instance operations
org         Show organization context
project     Manage projects
route       Alias for app route operations
task        Manage background tasks
version     Shows Wodby CLI version
```

### Public API commands

The top-level operational commands use the public Wodby REST API. API keys are scoped
to one organization, so commands that need `orgId` infer it automatically when the
current credentials expose a single organization.

All public API commands support:

```text
-o, --output table|json   Output format (default "table")
```

Mutating commands with complex bodies also support:

```text
    --data string   JSON request body
-f, --file string   Path to JSON request body
```

Commands that start asynchronous work generally support:

```text
    --wait               Wait for the created task or deployment to finish
    --timeout duration   Maximum time to wait (default 10m0s)
```

#### Organization and projects

```bash
wodby org current
wodby project list
wodby project get 123
```

#### Environments

```bash
wodby env list
wodby env get 123
wodby env create --name prod-eu --title "Production EU" --type prod
wodby env update 123 --name prod-eu --title "Production EU" --type prod
wodby env delete 123 --yes
```

#### Apps, instances, services, and routes

```bash
wodby app list
wodby app get 123
wodby app status 123

wodby app instance list --app 123
wodby app instance get 456
wodby app instance status 456

wodby instance list --app 123
wodby instance status 456

wodby app service list --instance 456
wodby app service get 789
wodby app service update 789 --replicas 2
wodby app service action 789 cache-clear --wait

wodby app route list --instance 456
wodby app route get 321
wodby app route create --service 789 --host example.com --port 80 --primary
wodby app route update 321 --disabled
wodby app route delete 321 --yes

wodby route list --instance 456
```

#### Builds and deployments

```bash
wodby build list --instance 456
wodby build get 123
wodby build deploy 123 --wait
wodby build void 123 --yes
wodby build registry-login 123 --host registry.example.com

wodby deployment list --instance 456
wodby deployment get 123
wodby deployment create --service 789 --force --wait
wodby deployment redeploy 123 --wait
wodby deployment wait 123
```

#### Backups, imports, and tasks

```bash
wodby backup list --instance 456
wodby backup get 123
wodby backup create --service 789 --integration 12 --bucket backups --wait

wodby import list --instance 456
wodby import get 123
wodby import create --service 789 --source url --url https://example.com/archive.tar --wait

wodby task list --instance 456 --statuses pending,in_progress
wodby task get 123
wodby task wait 123
wodby task logs 123
wodby task cancel 123 --yes
wodby task repeat 123 --force --wait
```

### CLI reference docs

The generated CLI reference is built from the Cobra command tree.

Generate it locally:

```bash
make cli-docs
```

Generated files are written to `out/docs/cli-reference`.

To update the public docs repository, set machine-user credentials and run:

```bash
export MACHINE_USER_API_TOKEN=...
export MACHINE_USER=...
export MACHINE_USER_EMAIL=...
make docs-update
```

The update script clones `wodby/docs`, replaces `2.0/docs/dev/cli-reference`,
ensures the MkDocs nav contains `CLI reference`, commits the changes, and pushes
to the docs `master` branch.

### `wodby ci`

The `ci` namespace manages the full build pipeline against Wodby:

```text
build       Build images
deploy      Deploy build to Wodby
init        Initialize config for CI process
release     Push images
run         Run container
```

#### `wodby ci init`

Initializes the local CI config, creates or loads the app build, logs in to the Docker registry, and prepares the working directory.

```bash
wodby ci init [OPTIONS] WODBY_APP_SERVICE_ID|WODBY_BUILD_ID
```

Flags:

```text
-i, --build-id string   Custom build id (used if can't identify automatically)
-n, --build-num int     Custom build number (used if can't identify automatically)
-c, --context string    Build context (default: current directory)
    --dind              Use data container for sharing files between commands
    --fix-permissions   Fix codebase permissions explicitly
    --provider string   Custom build provider name (used if can't identify automatically)
```

Notes:

- `wodby ci init` requires `WODBY_API_KEY` to be set.
- It reads `.wodby/post-deployment.yml` from the build context and attaches it to the build when present.
- It changes codebase permissions only when `--fix-permissions` is passed explicitly.

#### `wodby ci build`

Builds one or more services defined by the Wodby app build config. If no services are specified, it attempts to build all services.

```bash
wodby ci build [OPTIONS] [SERVICE]...
```

Flags:

```text
    --build-arg stringArray       Additional build argument in the 'NAME=VALUE' format. Repeatable
    --build-arg-env stringArray   Environment variable name to forward as a docker build argument. Repeatable
    --cache-backend string        Build cache backend: auto, local, registry, none (default "auto")
    --cache-dir string            Build cache directory for local backend
    --cache-ref string            Build cache reference for registry backend
    --cache-mode string           Build cache export mode (default "max")
    --cache-from stringArray      Additional buildx cache source. Repeatable. Advanced override
    --cache-to stringArray        Additional buildx cache destination. Repeatable. Advanced override
-f, --dockerfile string           Relative path to dockerfile
    --from string                 Relative path to codebase (default ".")
    --to string                   Codebase destination path in container (default ".")
```

Capabilities:

- Uses a service-specific Dockerfile from the build context when available.
- Falls back to the Dockerfile and `.dockerignore` provided by the Wodby app service config.
- Generates a default Dockerfile when neither is provided.
- Supports forwarding explicit build args and environment variables into Docker builds.
- Uses `docker buildx build --load` so built images remain available for `wodby ci release`.
- `--cache-dir` is enough to enable local cache for non-DIND builds.
- In `--dind` mode, `--cache-backend auto` defaults to registry-backed cache refs per service.
- `--cache-from` and `--cache-to` remain available as low-level buildx overrides.

#### `wodby ci release`

Pushes previously built images to the configured registry.

```bash
wodby ci release [SERVICE...]
```

Flags:

```text
-b, --branch-tag             Additionally push tag with the current git branch name
-l, --latest-branch string   Update latest tag when built from this branch (default "master")
```

If no services are provided, all built services are released.

#### `wodby ci deploy`

Deploys released images to Wodby.

```bash
wodby ci deploy [SERVICE...]
```

Flags:

```text
    --skip-post-deploy   Skip post deployment scripts execution
```

If no services are provided, all released services are deployed.

#### `wodby ci run`

Runs a one-off container using either a service image from the Wodby build config or an explicitly supplied image.

```bash
wodby ci run [OPTIONS] -s SERVICE | -i IMAGE COMMAND [ARG...]
```

Flags:

```text
    --entrypoint string   Entrypoint
-e, --env strings         Environment variables
    --env-file string     Env file
-i, --image string        Image
-p, --path string         Working dir (relative path)
-s, --service string      Service
-u, --user string         User
-v, --volume strings      Volumes
```

Notes:

- When `wodby ci run` bind-mounts the CI workspace, it uses the current process `uid:gid` unless `--user` is specified explicitly.
- If the current process user is `1000:1000`, `wodby ci run` leaves the image default user unchanged.
- When the effective Docker user is numeric (for example `1001:1001`), `wodby ci run` clears the image `ENTRYPOINT` unless `--entrypoint` is set explicitly.
- In `--dind` mode, no automatic host user mapping is applied because the codebase is mounted from the data container instead of the host filesystem.

If neither `--service` nor `--image` is provided, the CLI falls back to the main service image from the Wodby app build config.

### Build info auto-detection

When you run `wodby ci init`, the CLI auto-detects build and git metadata from supported CI providers:

- CircleCI
- GitLab CI
- GitHub Actions

If no supported CI environment is detected, it falls back to local git metadata. When CI metadata cannot be derived automatically, provide it explicitly with `--build-num`, `--build-id`, and `--provider`.

### Typical flow

```bash
wodby ci init 12345
wodby ci build php nginx
wodby ci release php nginx
wodby ci deploy php nginx
```
