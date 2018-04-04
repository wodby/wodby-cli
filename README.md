# Wodby CLI

This project provides a unified command line interface to [wodby.com](https://wodby.com).

## Install

Fetch the [latest release](https://github.com/wodby/wodby-cli/releases) for your platform:

#### Linux

```bash
curl -L https://github.com/wodby/wodby-cli/releases/download/$(curl -s https://api.github.com/repos/wodby/wodby-cli/releases/latest | grep tag_name | cut -d '"' -f 4)/wodby-linux-amd64.tar.gz | tar xvz
chmod +x wodby
sudo mv wodby /usr/local/bin/wodby
```

#### macOS

```bash
curl -L https://github.com/wodby/wodby-cli/releases/download/$(curl -s https://api.github.com/repos/wodby/wodby-cli/releases/latest | grep tag_name | cut -d '"' -f 4)/wodby-darwin-amd64.tar.gz | tar xvz
chmod +x wodby
sudo mv wodby /usr/local/bin/wodby
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
        init WODBY_INSTANCE_UUID
        run COMMAND
        build SERVICE/IMAGE
        release
        deploy
    help         Help about any command
    version      Shows Wodby CLI version

Flags:
      --api-key string      API key
      --api-prefix string   API prefix (default "api/v2")
      --api-proto string    API protocol (default "https")
      --dind                Docker in docker mode (for init)
  -h, --help                help for wodby
  -v, --verbose             Verbose output

Use "wodby [command] --help" for more information about a command.
```
