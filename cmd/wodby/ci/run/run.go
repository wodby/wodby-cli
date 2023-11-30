package run

import (
	"path"

	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
)

type options struct {
	service    string
	image      string
	volumes    []string
	env        []string
	envFile    string
	user       string
	entrypoint string
	path       string
}

var opts options
var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "run [OPTIONS] -s SERVICE | -i IMAGE",
	Short: "Run container",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		v.SetConfigFile(path.Join(viper.GetString("ci_config_path")))
		err := v.ReadInConfig()
		if err != nil {
			return errors.WithStack(err)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		config := new(types.Config)
		err := v.Unmarshal(config)
		if err != nil {
			return errors.WithStack(err)
		}

		var image string
		if opts.service != "" {
			for _, appServiceBuildConfig := range config.AppBuild.Config.Services {
				if appServiceBuildConfig.Name == opts.service {
					image = appServiceBuildConfig.Image
					break
				}
			}
			if image == "" {
				return errors.New(fmt.Sprintf("Couldn't find service %s", opts.service))
			}
		} else if opts.image != "" {
			image = opts.image
		} else {
			fmt.Println("No service or image provided, using main service")
			for _, appServiceBuildConfig := range config.AppBuild.Config.Services {
				if appServiceBuildConfig.Main {
					fmt.Printf("Main service found: %s\n", appServiceBuildConfig.Name)
					image = appServiceBuildConfig.Image
					break
				}
			}
		}

		runConfig := docker.RunConfig{
			Image:      image,
			Volumes:    opts.volumes,
			Env:        opts.env,
			EnvFile:    opts.envFile,
			User:       opts.user,
			Entrypoint: opts.entrypoint,
		}

		dockerClient := docker.NewClient()
		workingDir, err := dockerClient.GetImageWorkingDir(image)
		if err != nil {
			return errors.WithStack(err)
		}

		runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", config.Context, workingDir))

		if opts.path != "" {
			runConfig.WorkDir = fmt.Sprintf("%s/%s", workingDir, opts.path)
		}

		err = dockerClient.Run(args, runConfig)
		if err != nil {
			return errors.WithStack(err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "Entrypoint")
	Cmd.Flags().StringVarP(&opts.service, "service", "s", "", "Service")
	Cmd.Flags().StringVarP(&opts.image, "image", "i", "", "Image")
	Cmd.Flags().StringSliceVarP(&opts.volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&opts.env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Env file")
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "User")
	Cmd.Flags().StringVarP(&opts.path, "path", "p", "", "Working dir (relative path)")
}
