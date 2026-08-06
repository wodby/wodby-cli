package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/pkg/errors"
)

type options struct {
	from            string
	to              string
	dockerfile      string
	services        []string
	buildArgs       []string
	buildArgEnvVars []string
	cacheBackend    string
	cacheDir        string
	cacheRef        string
	cacheMode       string
	cacheFrom       []string
	cacheTo         []string
}

var opts options

const DefaultDockerignore = `.git
.gitignore
.dockerignore`

const DefaultDockerfileTpl = `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_FROM
ARG COPY_TO
COPY --chown={{.DefaultUserOwnership}} ${COPY_FROM} ${COPY_TO}`

var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "build [OPTIONS] [SERVICE]...",
	Short: "Build images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args
		v.SetConfigFile(filepath.Join(viper.GetString("ci_config_path")))
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

		logger := log.WithField("stage", "build")
		log.SetOutput(os.Stdout)
		if viper.GetBool("verbose") {
			log.SetLevel(log.DebugLevel)
		}

		var appServiceBuildConfigs []*types.AppServiceBuildConfig
		if len(opts.services) > 0 {
			logger.Info("Validating services")
			for _, svc := range opts.services {
				found := false
				for _, appServiceBuildConfig := range config.AppBuild.Config.Services {
					if svc == appServiceBuildConfig.Name {
						found = true
						appServiceBuildConfigs = append(appServiceBuildConfigs, appServiceBuildConfig)
						break
					}
				}
				if !found {
					return errors.New(fmt.Sprintf("Couldn't find service %s", svc))
				}
			}
		} else {
			logger.Info("No services specified, trying to build all services")
			for _, appServiceBuildConfig := range config.AppBuild.Config.Services {
				appServiceBuildConfigs = append(appServiceBuildConfigs, appServiceBuildConfig)
			}
		}
		appServiceBuildConfigs = prioritizeMainService(appServiceBuildConfigs)

		context := config.Context
		if config.DataContainer != "" {
			fmt.Println("Synchronizing data container")

			context = dataContainerContextPath(config.DataContainer)
			if err := os.RemoveAll(context); err != nil {
				return errors.WithStack(err)
			}
			output, err := exec.Command(
				"docker",
				"cp",
				fmt.Sprintf("%s:%s", config.DataContainer, dataContainerWorkingDirContents(config.WorkingDir)),
				context,
			).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}
		}

		dockerClient := docker.NewClient()
		var dockerfile string
		var tag string

		for _, appServiceBuildConfig := range appServiceBuildConfigs {
			buildArgs := make(map[string]string)
			var redactValues []string
			buildArgs["COPY_FROM"] = opts.from
			buildArgs["WODBY_BASE_IMAGE"] = appServiceBuildConfig.Image
			buildFiles := newBuildFiles(context, appServiceBuildConfig.Name, opts.dockerfile)
			cacheFrom, cacheTo, err := resolveCacheOptions(config, appServiceBuildConfig.Name, opts)
			if err != nil {
				return errors.WithStack(err)
			}

			for _, buildArg := range opts.buildArgs {
				name, value, err := parseBuildArg(buildArg)
				if err != nil {
					return errors.WithStack(err)
				}
				buildArgs[name] = value
			}
			for _, envName := range opts.buildArgEnvVars {
				value, ok := os.LookupEnv(envName)
				if !ok {
					return errors.Errorf("environment variable %s is not set", envName)
				}
				buildArgs[envName] = value
			}

			if opts.dockerfile != "" {
				fmt.Println("Using specified Dockerfile")
				d, err := os.ReadFile(buildFiles.dockerfilePath)
				if err != nil {
					return errors.WithStack(err)
				}
				dockerfile = string(d)
				if err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, opts.to, logger, &redactValues); err != nil {
					return errors.WithStack(err)
				}
			} else if fileExists(buildFiles.dockerfilePath) {
				fmt.Printf("Using Dockerfile from context: %s\n", buildFiles.dockerfilePath)
				d, err := os.ReadFile(buildFiles.dockerfilePath)
				if err != nil {
					return errors.WithStack(err)
				}
				dockerfile = string(d)
				if err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, opts.to, logger, &redactValues); err != nil {
					return errors.WithStack(err)
				}
			} else {
				if appServiceBuildConfig.Dockerfile != nil {
					fmt.Println("Dockerfile provided by app service")
					dockerfile = *appServiceBuildConfig.Dockerfile
					if err := addDockerfileBuildArgs(buildArgs, dockerfile, appServiceBuildConfig, opts.to, logger, &redactValues); err != nil {
						return errors.WithStack(err)
					}
				} else {
					fmt.Println("No Dockerfile provided by app service, using the default")
					buildArgs["COPY_TO"] = opts.to
					// Replace default image user in dockerfile template.
					defaultUser, err := dockerClient.GetImageDefaultUser(appServiceBuildConfig.Image)
					if err != nil {
						return errors.WithStack(err)
					}
					t, err := template.New("Dockerfile").Parse(DefaultDockerfileTpl)
					if err != nil {
						return errors.WithStack(err)
					}
					data := struct{ DefaultUserOwnership string }{DefaultUserOwnership: docker.ChownSpec(defaultUser)}
					var tpl bytes.Buffer
					if err := t.Execute(&tpl, data); err != nil {
						return errors.WithStack(err)
					}
					dockerfile = tpl.String()
				}
			}

			var dockerignore string
			if appServiceBuildConfig.Dockerignore != nil {
				fmt.Println(".dockerignore provided by app service")
				dockerignore = *appServiceBuildConfig.Dockerignore
			} else {
				fmt.Println("No .dockerignore provided by app service, using default")
				dockerignore = DefaultDockerignore
			}

			var cleanUpDockerfile bool
			var cleanUpDockerignore bool
			if !fileExists(buildFiles.dockerfilePath) {
				cleanUpDockerfile = true
				fmt.Printf("Creating temporary Dockerfile: %s\n", buildFiles.dockerfilePath)
				err = os.WriteFile(buildFiles.dockerfilePath, []byte(dockerfile), 0600)
				if err != nil {
					_ = os.Remove(buildFiles.dockerfilePath)
					return errors.WithStack(err)
				}
			}
			if !fileExists(buildFiles.dockerignorePath) {
				// Exclude dockerignore and dockerfile.
				dockerignore = fmt.Sprintf("%s\n%s\n%s", dockerignore, buildFiles.dockerfileName, buildFiles.dockerignoreName)
				fmt.Printf("Creating temporary .dockerignore: %s\n", buildFiles.dockerignorePath)
				err = os.WriteFile(buildFiles.dockerignorePath, []byte(dockerignore), 0600)
				if err != nil {
					if cleanUpDockerfile {
						_ = os.Remove(buildFiles.dockerfilePath)
					}
					_ = os.Remove(buildFiles.dockerignorePath)
					return errors.WithStack(err)
				}
				cleanUpDockerignore = true
			}

			tag = appBuildImageTag(config, appServiceBuildConfig.Name)
			err = dockerClient.Build(docker.BuildConfig{
				Dockerfile:   buildFiles.dockerfilePath,
				Tags:         []string{tag},
				Context:      context,
				BuildArgs:    buildArgs,
				CacheFrom:    cacheFrom,
				CacheTo:      cacheTo,
				Load:         true,
				RedactValues: redactValues,
			})
			if err != nil {
				if cleanUpDockerfile {
					fmt.Println("Cleaning up Dockerfile")
					_ = os.Remove(buildFiles.dockerfilePath)
				}
				if cleanUpDockerignore {
					fmt.Println("Cleaning up .dockerignore")
					_ = os.Remove(buildFiles.dockerignorePath)
				}
				return errors.WithStack(err)
			}
			config.BuiltServices = append(config.BuiltServices, types.BuiltService{
				Name:  appServiceBuildConfig.Name,
				Image: tag,
			})

			if cleanUpDockerfile {
				fmt.Println("Cleaning up dockerfile")
				err = os.Remove(buildFiles.dockerfilePath)
				if err != nil {
					return errors.WithStack(err)
				}
			}
			if cleanUpDockerignore {
				fmt.Println("Cleaning up dockerignore")
				err = os.Remove(buildFiles.dockerignorePath)
				if err != nil {
					return errors.WithStack(err)
				}
			}
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = os.WriteFile(filepath.Join(viper.GetString("ci_config_path")), content, 0600)
		if err != nil {
			return errors.WithStack(err)
		}

		return nil
	},
}

// appBuildImageTag keeps the human-facing launch number while including the
// unique app-build ID so parallel workflows for one service cannot overwrite
// each other's registry tag.
func appBuildImageTag(config *types.Config, serviceName string) string {
	return fmt.Sprintf(
		"%s/%s:%s-%d-%s",
		config.AppBuild.Config.RegistryHost,
		config.AppBuild.Config.RegistryRepository,
		serviceName,
		config.AppBuild.Number,
		config.AppBuild.ID.String(),
	)
}

func containsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func dataContainerContextPath(dataContainer string) string {
	return fmt.Sprintf("/tmp/wodby-build-%s", dataContainer)
}

func dataContainerWorkingDirContents(workingDir string) string {
	cleaned := path.Clean(workingDir)
	if cleaned == "." || cleaned == "/" {
		return "/."
	}

	return cleaned + "/."
}

func prioritizeMainService(services []*types.AppServiceBuildConfig) []*types.AppServiceBuildConfig {
	for i, service := range services {
		if service.Main {
			if i == 0 {
				return services
			}

			ordered := make([]*types.AppServiceBuildConfig, 0, len(services))
			ordered = append(ordered, service)
			ordered = append(ordered, services[:i]...)
			ordered = append(ordered, services[i+1:]...)

			return ordered
		}
	}

	return services
}

type tempBuildFiles struct {
	dockerfileName   string
	dockerfilePath   string
	dockerignoreName string
	dockerignorePath string
}

func newBuildFiles(context string, serviceName string, dockerfileOverride string) tempBuildFiles {
	dockerfilePath := filepath.Join(context, fmt.Sprintf("%s_Dockerfile", serviceName))
	if dockerfileOverride != "" {
		dockerfilePath = filepath.Join(context, dockerfileOverride)
	}
	dockerfileName := filepath.Base(dockerfilePath)
	dockerignoreName := fmt.Sprintf("%s.dockerignore", dockerfileName)

	return tempBuildFiles{
		dockerfileName:   dockerfileName,
		dockerfilePath:   dockerfilePath,
		dockerignoreName: dockerignoreName,
		dockerignorePath: filepath.Join(filepath.Dir(dockerfilePath), dockerignoreName),
	}
}

func addDockerfileBuildArgs(buildArgs map[string]string, dockerfile string, appServiceBuildConfig *types.AppServiceBuildConfig, copyTo string, logger *log.Entry, redactValues *[]string) error {
	// Pass build args from dockerfile.
	argNames := dockerfileArgNames(dockerfile)
	logger.Debugf("Found %d ARGs in Dockerfile", len(argNames))
	for _, argName := range argNames {
		logger.Debugf("Arg name: %s", argName)
		if !containsString([]string{"COPY_FROM", "WODBY_BASE_IMAGE"}, argName) {
			if argName == "COPY_TO" {
				buildArgs["COPY_TO"] = copyTo
			} else {
				for _, arg := range appServiceBuildConfig.Args {
					logger.Debugf("Build arg %s", arg.Name)
					if argName == arg.Name {
						if arg.Secret {
							value, ok := os.LookupEnv(arg.Name)
							if !ok {
								return errors.Errorf("secret build arg %s requires environment variable %s", arg.Name, arg.Name)
							}
							buildArgs[argName] = ""
							*redactValues = append(*redactValues, value)
						} else {
							buildArgs[argName] = arg.Value
						}
					}
				}
			}
		}
	}

	return nil
}

func dockerfileArgNames(dockerfile string) []string {
	var argNames []string
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !strings.EqualFold(fields[0], "ARG") {
			continue
		}

		argName := fields[1]
		if idx := strings.Index(argName, "="); idx >= 0 {
			argName = argName[:idx]
		}
		if argName != "" {
			argNames = append(argNames, argName)
		}
	}

	return argNames
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func parseBuildArg(raw string) (string, string, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", errors.Errorf("invalid build arg %q, expected NAME=VALUE", raw)
	}

	return parts[0], parts[1], nil
}

func resolveCacheOptions(config *types.Config, serviceName string, opts options) ([]string, []string, error) {
	if len(opts.cacheFrom) != 0 || len(opts.cacheTo) != 0 {
		return opts.cacheFrom, opts.cacheTo, nil
	}

	backend := opts.cacheBackend
	if backend == "" {
		backend = "auto"
	}

	mode := opts.cacheMode
	if mode == "" {
		mode = "max"
	}

	switch backend {
	case "none":
		return nil, nil, nil
	case "auto":
		if opts.cacheDir != "" {
			return localCacheOptions(opts.cacheDir, mode), localCacheDestinations(opts.cacheDir, mode), nil
		}
		if config.DataContainer != "" {
			ref := cacheRef(config, serviceName, opts.cacheRef)
			return registryCacheOptions(ref, mode), registryCacheDestinations(ref, mode), nil
		}
		return nil, nil, nil
	case "local":
		dir := opts.cacheDir
		if dir == "" {
			dir = ".buildx-cache"
		}
		return localCacheOptions(dir, mode), localCacheDestinations(dir, mode), nil
	case "registry":
		ref := cacheRef(config, serviceName, opts.cacheRef)
		return registryCacheOptions(ref, mode), registryCacheDestinations(ref, mode), nil
	default:
		return nil, nil, errors.Errorf("unsupported cache backend %q", backend)
	}
}

func cacheRef(config *types.Config, serviceName string, explicitRef string) string {
	if explicitRef != "" {
		return explicitRef
	}
	return fmt.Sprintf("%s/%s:%s-buildcache", config.AppBuild.Config.RegistryHost, config.AppBuild.Config.RegistryRepository, serviceName)
}

func localCacheOptions(dir string, mode string) []string {
	return []string{fmt.Sprintf("type=local,src=%s", dir)}
}

func localCacheDestinations(dir string, mode string) []string {
	return []string{fmt.Sprintf("type=local,dest=%s,mode=%s", dir, mode)}
}

func registryCacheOptions(ref string, mode string) []string {
	return []string{fmt.Sprintf("type=registry,ref=%s", ref)}
}

func registryCacheDestinations(ref string, mode string) []string {
	return []string{fmt.Sprintf("type=registry,ref=%s,mode=%s", ref, mode)}
}

func init() {
	Cmd.Flags().StringVar(&opts.from, "from", ".", "Relative path to codebase")
	Cmd.Flags().StringVar(&opts.to, "to", ".", "Codebase destination path in container")
	Cmd.Flags().StringVarP(&opts.dockerfile, "dockerfile", "f", "", "Relative path to dockerfile")
	Cmd.Flags().StringArrayVar(&opts.buildArgs, "build-arg", nil, "Additional build argument in the 'NAME=VALUE' format. Repeatable")
	Cmd.Flags().StringArrayVar(&opts.buildArgEnvVars, "build-arg-env", nil, "Environment variable name to forward as a docker build argument. Repeatable")
	Cmd.Flags().StringVar(&opts.cacheBackend, "cache-backend", "auto", "Build cache backend: auto, local, registry, none")
	Cmd.Flags().StringVar(&opts.cacheDir, "cache-dir", "", "Build cache directory for local backend")
	Cmd.Flags().StringVar(&opts.cacheRef, "cache-ref", "", "Build cache reference for registry backend")
	Cmd.Flags().StringVar(&opts.cacheMode, "cache-mode", "max", "Build cache export mode")
	Cmd.Flags().StringArrayVar(&opts.cacheFrom, "cache-from", nil, "Additional buildx cache source. Repeatable")
	Cmd.Flags().StringArrayVar(&opts.cacheTo, "cache-to", nil, "Additional buildx cache destination. Repeatable")
}
