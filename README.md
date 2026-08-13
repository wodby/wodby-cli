# Wodby CLI 1.x

[![Build Status](https://github.com/wodby/wodby-cli/workflows/Build/badge.svg)](https://github.com/wodby/wodby-cli/actions)
[![Docker Pulls](https://img.shields.io/docker/pulls/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Stars](https://img.shields.io/docker/stars/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Layers](https://images.microbadger.com/badges/image/wodby/wodby-cli.svg)](https://microbadger.com/images/wodby/wodby-cli)

This branch contains Wodby CLI 1.x, the maintenance line for Wodby 1. It
preserves the existing Wodby 1 commands and API behavior while receiving
compatible dependency, CI, and reliability fixes.

## Wodby and CLI versions

The CLI major version must match the Wodby platform major version:

| Wodby platform | CLI releases | Source branch | Status |
| --- | --- | --- | --- |
| Wodby 1 | [1.x](https://github.com/wodby/wodby-cli/releases/tag/1.0.2) | [`master`](https://github.com/wodby/wodby-cli/tree/master) | Maintenance |
| Wodby 2 | [2.x](https://github.com/wodby/wodby-cli/releases/latest) | [`2.0`](https://github.com/wodby/wodby-cli/tree/2.0) | Active development |

GitHub's **Latest** release tracks Wodby 2. Wodby 1 users should install an
explicit 1.x release, currently [1.0.2](https://github.com/wodby/wodby-cli/releases/tag/1.0.2),
instead of using the `/releases/latest` URL.

## Install

Install the current Wodby 1 release on Linux or macOS:

```bash
case "$(uname -s)" in
  Linux) WODBY_CLI_OS=linux ;;
  Darwin) WODBY_CLI_OS=darwin ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) WODBY_CLI_ARCH=amd64 ;;
  arm64|aarch64) WODBY_CLI_ARCH=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac

WODBY_CLI_VERSION=1.0.2
curl -fsSL "https://github.com/wodby/wodby-cli/releases/download/${WODBY_CLI_VERSION}/wodby-${WODBY_CLI_OS}-${WODBY_CLI_ARCH}.tar.gz" \
  | sudo tar xz -C /usr/local/bin
```

Windows users can download the matching archive from the
[1.0.2 release](https://github.com/wodby/wodby-cli/releases/tag/1.0.2).

## Usage

You can run the Wodby CLI in your shell by typing `wodby`.

### Commands

The current output of `wodby` is as follows:

```
CLI client for Wodby

Usage:
    wodby [command]

Available Commands:
    ci
        init WODBY_INSTANCE_UUID
        run COMMAND
        build SERVICE/IMAGE
        release
        deploy
    help         Help about any command
    version      Shows Wodby CLI version

Flags:
      --api-key string          API key
      --api-prefix string       API prefix (default "api/v2")
      --api-proto string        API protocol (default "https")
      --ci-config-path string   Path to CI config (defaut "/tmp/.wodby-ci.json")
      --dind                    Docker in docker mode (for init)
  -h, --help                    Help for wodby
  -v, --verbose                 Verbose output

Use "wodby [command] --help" for more information about a command.
```
