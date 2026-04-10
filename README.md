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
--ci-config-path string   Path to CI config (default "/tmp/.wodby-ci.json")
--verbose                 Verbose output
```

### Commands

`wodby` currently exposes:

```text
ci          CI-oriented build, release, deploy, and run workflows
completion  Generate shell completion scripts
help        Help about any command
version     Shows Wodby CLI version
```

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
    --fix-permissions   Fix codebase permissions
    --provider string   Custom build provider name (used if can't identify automatically)
```

Notes:

- `wodby ci init` requires `WODBY_API_KEY` to be set.
- It reads `.wodby/post-deployment.yml` from the build context and attaches it to the build when present.
- In managed CI environments it can fix file permissions automatically for managed services.

#### `wodby ci build`

Builds one or more services defined by the Wodby app build config. If no services are specified, it attempts to build all services.

```bash
wodby ci build [OPTIONS] [SERVICE]...
```

Flags:

```text
    --build-arg stringArray       Additional build argument in the 'NAME=VALUE' format. Repeatable
    --build-arg-env stringArray   Environment variable name to forward as a docker build argument. Repeatable
-f, --dockerfile string           Relative path to dockerfile
    --from string                 Relative path to codebase (default ".")
    --to string                   Codebase destination path in container (default ".")
```

Capabilities:

- Uses a service-specific Dockerfile from the build context when available.
- Falls back to the Dockerfile and `.dockerignore` provided by the Wodby app service config.
- Generates a default Dockerfile when neither is provided.
- Supports forwarding explicit build args and environment variables into Docker builds.

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
