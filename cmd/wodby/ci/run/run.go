package run

import (
	"fmt"
	"path"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/cicache"
	"github.com/wodby/wodby-cli/pkg/ciuser"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
)

type options struct {
	services   []string
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

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Run container",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		v.SetConfigFile(path.Join(viper.GetString("ci_config_path")))

		err := v.ReadInConfig()
		if err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		config := new(config.Config)

		err := v.Unmarshal(config)
		if err != nil {
			return err
		}

		var images []string

		if len(opts.services) != 0 {
			for _, svc := range opts.services {
				// Find services by prefix.
				if svc[len(svc)-1] == '-' {
					matchingServices, err := config.FindServicesByPrefix(svc)

					if err != nil {
						return err
					}

					for _, service := range matchingServices {
						fmt.Printf("Found matching svc %s\n", service.Name)
						images = append(images, service.Image)
					}
				} else {
					service, err := config.FindService(svc)

					if err != nil {
						return err
					}

					images = append(images, service.Image)
				}
			}
		} else if opts.image == "" {
			images = append(images, config.BuildConfig.Services[config.BuildConfig.Default].Image)
		} else {
			images = append(images, opts.image)
		}

		if len(images) == 0 {
			return errors.New("No valid images found for this run")
		}

		for _, image := range images {
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
				return err
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

			runConfig.User = runUserOverride(opts.user)
			runConfig.ClearEntrypoint = shouldClearImageEntrypoint(opts.entrypoint, runConfig.User)

			explicitEnv, err := explicitEnvironmentNames(opts.env, opts.envFile)
			if err != nil {
				return err
			}
			strictCache := cacheConfigurationIsExplicit(opts.cache)
			cacheNames, err := resolveRunCacheProfileNames(opts.cache, opts.noCache, opts.user, image, imageConfig.Labels)
			if err != nil {
				if cacheErr := handleCacheFailure("configuration", strictCache, err); cacheErr != nil {
					return cacheErr
				}
				cacheNames = nil
			}
			if len(cacheNames) > 0 {
				cacheUser := runConfig.User
				if cacheUser == "" && config.DataContainer == "" {
					uid, gid, identityErr := dockerClient.ResolveImageUserIdentity(image, imageConfig.User)
					if identityErr != nil {
						if cacheErr := handleCacheFailure("user resolution", strictCache, identityErr); cacheErr != nil {
							return cacheErr
						}
						cacheNames = nil
					} else {
						cacheUser = fmt.Sprintf("%d:%d", uid, gid)
					}
				}

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
						cacheUser,
					)
				}
				if cacheErr == nil && config.DataContainer == "" && len(cacheNames) > 0 && cacheUser != "" && !ciuser.CanChownDirectories() {
					cacheErr = prepareNativeCacheOwnership(dockerClient, image, cacheNames, hostHome, cacheRoot, cacheUser)
				}
				if cacheErr == nil && config.DataContainer != "" && len(cacheNames) > 0 {
					cacheErr = cicache.PrepareDataContainerProfiles(config.DataContainer, cacheNames)
				}
				if cacheErr != nil {
					if handledErr := handleCacheFailure("setup", strictCache, cacheErr); handledErr != nil {
						return handledErr
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
						return cacheErr
					}
				}
			}
			if runErr != nil {
				return runErr
			}
		}

		return nil
	},
}

// runUserOverride returns only an explicitly requested Docker user. Wodby 1
// managed checkouts are prepared for their image user during initialization,
// so native and Docker-in-Docker commands must both preserve that image user.
func runUserOverride(explicitUser string) string {
	return explicitUser
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

func init() {
	Cmd.Flags().StringVar(&opts.entrypoint, "entrypoint", "", "Entrypoint")
	Cmd.Flags().StringSliceVarP(&opts.services, "services", "s", []string{}, "Service")
	Cmd.Flags().StringVarP(&opts.image, "image", "i", "", "Image")
	Cmd.Flags().StringSliceVarP(&opts.volumes, "volume", "v", []string{}, "Volumes")
	Cmd.Flags().StringSliceVarP(&opts.env, "env", "e", []string{}, "Environment variables")
	Cmd.Flags().StringVarP(&opts.user, "user", "u", "", "User (defaults to the image user)")
	Cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Env file")
	Cmd.Flags().StringVarP(&opts.path, "path", "p", "", "Working dir (relative path)")
	Cmd.Flags().StringSliceVar(&opts.cache, "cache", []string{}, "Cache profiles to enable instead of auto-detection (npm, composer, bundler, uv)")
	Cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable automatic cache mounts")
}
