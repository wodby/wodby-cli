package run

import (
	"path"
	"strings"

	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/cicache"
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
	cache      []string
	noCache    bool
}

var opts options
var v = viper.New()
var currentHostUser = defaultCurrentHostUser
var workspaceOwner = defaultWorkspaceOwner

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
			Entrypoint: opts.entrypoint,
		}

		dockerClient := docker.NewClient()
		imageConfig, err := dockerClient.GetImageConfig(image)
		if err != nil {
			return errors.WithStack(err)
		}
		workingDir := imageConfig.WorkingDir

		if config.DataContainer != "" {
			runConfig.VolumesFrom = []string{config.DataContainer}
			if config.WorkingDir != "" {
				workingDir = config.WorkingDir
			}
		} else {
			runConfig.Volumes = append(runConfig.Volumes, fmt.Sprintf("%s:%s", config.Context, workingDir))
		}

		runConfig.User, err = resolveRunUser(opts.user, config)
		if err != nil {
			return errors.WithStack(err)
		}
		runConfig.ClearEntrypoint = shouldClearImageEntrypoint(opts.entrypoint, runConfig.User)

		explicitEnv, err := explicitEnvironmentNames(opts.env, opts.envFile)
		if err != nil {
			return errors.WithStack(err)
		}
		if opts.user == "" && config.DataContainer == "" && runConfig.User != "" {
			runConfig.Env = withMappedUserHome(runConfig.Env, explicitEnv)
		}

		strictCache := cacheConfigurationIsExplicit(opts.cache)
		cacheNames, err := resolveRunCacheProfileNames(
			opts.cache,
			opts.noCache,
			opts.user,
			image,
			imageConfig.Labels,
		)
		if err != nil {
			if cacheErr := handleCacheFailure("configuration", strictCache, err); cacheErr != nil {
				return errors.WithStack(cacheErr)
			}
			cacheNames = nil
		}
		if len(cacheNames) > 0 {
			cacheConfig := runConfig
			cacheConfig.Env = append([]string(nil), runConfig.Env...)
			cacheConfig.Volumes = append([]string(nil), runConfig.Volumes...)
			cacheConfig.VolumesFrom = append([]string(nil), runConfig.VolumesFrom...)

			hostHome, cacheRoot, cacheErr := resolveHostCacheStorage(config.Context, config.DataContainer != "")
			if cacheErr == nil {
				cacheNames, cacheErr = addCacheProfiles(
					&cacheConfig,
					cacheNames,
					explicitEnv,
					hostHome,
					cacheRoot,
					config.DataContainer != "",
					runConfig.User,
				)
			}
			if cacheErr == nil && config.DataContainer != "" && len(cacheNames) > 0 {
				cacheErr = cicache.PrepareDataContainerProfiles(config.DataContainer, cacheNames)
			}
			if cacheErr != nil {
				if handledErr := handleCacheFailure("setup", strictCache, cacheErr); handledErr != nil {
					return errors.WithStack(handledErr)
				}
				cacheNames = nil
			} else {
				runConfig = cacheConfig
			}
		}

		if opts.path != "" {
			runConfig.WorkDir = fmt.Sprintf("%s/%s", workingDir, opts.path)
		}
		runErr := dockerClient.Run(args, runConfig)
		if config.DataContainer != "" && len(cacheNames) > 0 {
			exportErr := cicache.ExportDataContainerProfiles(config.DataContainer, config.Context, cacheNames)
			if runErr == nil && exportErr != nil {
				if cacheErr := handleCacheFailure("export", strictCache, exportErr); cacheErr != nil {
					return errors.WithStack(cacheErr)
				}
			}
		}
		if runErr != nil {
			return errors.WithStack(runErr)
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
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "User (defaults to the workspace uid:gid for bind-mounted contexts)")
	Cmd.Flags().StringVarP(&opts.path, "path", "p", "", "Working dir (relative path)")
	Cmd.Flags().StringSliceVar(&opts.cache, "cache", []string{}, "Cache profiles to enable instead of auto-detection (npm, composer, bundler, uv)")
	Cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable automatic cache mounts")
}

func resolveRunUser(explicitUser string, config *types.Config) (string, error) {
	if explicitUser != "" {
		return explicitUser, nil
	}

	if config.DataContainer != "" {
		return "", nil
	}

	user, err := currentHostUser()
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(user, "0:") && config.Context != "" {
		owner, err := workspaceOwner(config.Context)
		if err != nil {
			return "", err
		}
		if owner != "" && !strings.HasPrefix(owner, "0:") {
			return owner, nil
		}
	}

	return user, nil
}

func shouldClearImageEntrypoint(explicitEntrypoint string, user string) bool {
	if explicitEntrypoint != "" || user == "" {
		return false
	}

	identity := user
	if idx := strings.Index(identity, ":"); idx >= 0 {
		identity = identity[:idx]
	}

	if identity == "" {
		return false
	}

	for _, r := range identity {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
