# Wodby CLI 2.x

[![Build Status](https://github.com/wodby/wodby-cli/workflows/Build/badge.svg)](https://github.com/wodby/wodby-cli/actions)
[![Docker Pulls](https://img.shields.io/docker/pulls/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Stars](https://img.shields.io/docker/stars/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)

This branch contains Wodby CLI 2.x, the actively developed command line
interface for Wodby 2.

## Wodby and CLI versions

The CLI major version must match the Wodby platform major version:

| Wodby platform | CLI releases | Source branch | Status |
| --- | --- | --- | --- |
| Wodby 1 | [1.x](https://github.com/wodby/wodby-cli/releases/tag/1.0.0) | [`master`](https://github.com/wodby/wodby-cli/tree/master) | Maintenance |
| Wodby 2 | [2.x](https://github.com/wodby/wodby-cli/releases/latest) | [`2.0`](https://github.com/wodby/wodby-cli/tree/2.0) | Active development |

GitHub's **Latest** release tracks Wodby 2. Wodby 1 users should install an
explicit 1.x release from the `master` line instead.

## Install

Fetch the [latest Wodby 2 release](https://github.com/wodby/wodby-cli/releases/latest)
for your platform:

#### Linux/macOS

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

export WODBY_CLI_LATEST_URL=$(curl -s https://api.github.com/repos/wodby/wodby-cli/releases/latest | grep "${WODBY_CLI_OS}-${WODBY_CLI_ARCH}" | grep browser_download_url | cut -d '"' -f 4)
wget -qO- "${WODBY_CLI_LATEST_URL}" | sudo tar xz -C /usr/local/bin
```

## Documentation

See the [Wodby CLI documentation](https://wodby.com/docs/2.0/dev/cli/) for usage
and examples, and the
[CLI reference](https://wodby.com/docs/2.0/cli/) for the full
command list.

## Authentication

Before using the CLI, set your Wodby API key:

```bash
export WODBY_API_KEY=...
```

After that, run the CLI with `wodby`.
