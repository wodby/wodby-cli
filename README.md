# Wodby CLI 2.0

[![Build Status](https://github.com/wodby/wodby-cli/workflows/Build/badge.svg)](https://github.com/wodby/wodby-cli/actions)
[![Docker Pulls](https://img.shields.io/docker/pulls/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Stars](https://img.shields.io/docker/stars/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)

This project provides a unified command line interface to Wodby 2.0

## Install

Fetch the [latest release](https://github.com/wodby/wodby-cli/releases) for your platform:

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
