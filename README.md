# Wodby CLI 2.0

[![Build Status](https://github.com/wodby/installer/workflows/Build/badge.svg)](https://github.com/wodby/installer/actions)
[![Docker Pulls](https://img.shields.io/docker/pulls/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Stars](https://img.shields.io/docker/stars/wodby/wodby-cli.svg)](https://hub.docker.com/r/wodby/wodby-cli)
[![Docker Layers](https://images.microbadger.com/badges/image/wodby/wodby-cli.svg)](https://microbadger.com/images/wodby/wodby-cli)

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

You can run the Wodby CLI in your shell by typing `wodby`.

### Commands

The current output of `wodby` is as follows:

```
CLI client for Wodby

Usage:
    wodby [command]

Available Commands:
    ci
        init WODBY_APP_INSTANCE_ID
        run COMMAND
        build SERVICE
        release SERVICE...
        deploy SERVICE...
    help         Help about any command
    version      Shows Wodby CLI version

Flags:
      --api-key string      API key
      --api-endpoint string API endpoint
  -h, --help                help for wodby
  -v, --verbose             Verbose output

Use "wodby [command] --help" for more information about a command.
```
