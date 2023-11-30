package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path"
	"regexp"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/pkg/errors"
)

type options struct {
	from       string
	to         string
	dockerfile string
	services   []string
}

var opts options

const DefaultDockerignore = `.git
.gitignore
.dockerignore`

const DefaultDockerfileTpl = `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_FROM
ARG COPY_TO
COPY --chown={{.DefaultUser}}:{{.DefaultUser}} ${COPY_FROM} ${COPY_TO}`

var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "build [OPTIONS] SERVICE...",
	Short: "Build images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("Missing required parameters")
		}
		opts.services = args
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

		logger := log.WithField("stage", "build")
		log.SetOutput(os.Stdout)
		if viper.GetBool("verbose") {
			log.SetLevel(log.DebugLevel)
		}

		var appServiceBuildConfigs []*types.AppServiceBuildConfig
		if len(opts.services) == 0 {
			return errors.New("At least one service must be specified for the build")
		} else {
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
		}

		context := v.GetString("context")
		dockerClient := docker.NewClient()
		var dockerfile string
		var tag string

		for _, appServiceBuildConfig := range appServiceBuildConfigs {
			buildArgs := make(map[string]string)
			buildArgs["COPY_FROM"] = opts.from
			buildArgs["WODBY_BASE_IMAGE"] = appServiceBuildConfig.Image

			for _, arg := range appServiceBuildConfig.Args {
				buildArgs[arg.Name] = arg.Value
			}

			// When user specified custom dockerfile.
			if opts.dockerfile != "" {
				fmt.Println("Using specified Dockerfile")
				d, err := os.ReadFile(context + "/" + opts.dockerfile)
				if err != nil {
					return errors.WithStack(err)
				}
				dockerfile = string(d)
			} else {
				if appServiceBuildConfig.Dockerfile != nil {
					fmt.Println("Dockerfile provided by app service")
					dockerfile = *appServiceBuildConfig.Dockerfile
					r, err := regexp.Compile("(?m)^ARG (.+)$")
					if err != nil {
						return errors.WithStack(err)
					}
					// Pass build args from dockerfile.
					allMatches := r.FindAllStringSubmatch(dockerfile, -1)
					for _, matches := range allMatches {
						if !containsString([]string{"COPY_FROM", "WODBY_BASE_IMAGE"}, matches[1]) {
							if matches[1] == "COPY_TO" {
								buildArgs["COPY_TO"] = opts.to
							} else {
								buildArgs[matches[1]] = ""
							}
						}
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
					data := struct{ DefaultUser string }{DefaultUser: defaultUser}
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
			dockerfileName := fmt.Sprintf("%s_Dockerfile", appServiceBuildConfig.Name)
			if _, err := os.Stat(dockerfileName); os.IsNotExist(err) {
				fmt.Printf("Creating temporary Dockerfile: %s\n", path.Join(context, dockerfileName))
				err = os.WriteFile(path.Join(context, dockerfileName), []byte(dockerfile), 0600)
				if err != nil {
					return errors.WithStack(err)
				}
				cleanUpDockerfile = true
			}
			dockerignoreName := fmt.Sprintf("%s.dockerignore", dockerfileName)
			if _, err := os.Stat(dockerignoreName); os.IsNotExist(err) {
				// Exclude dockerignore and dockerfile.
				dockerignore = fmt.Sprintf("%s\n%s\n%s", dockerignore, dockerfileName, dockerignoreName)
				fmt.Printf("Creating temporary .dockerignore: %s\n", path.Join(context, dockerignoreName))
				err = os.WriteFile(path.Join(context, dockerignoreName), []byte(dockerignore), 0600)
				if err != nil {
					return errors.WithStack(err)
				}
				cleanUpDockerignore = true
			}

			tag = fmt.Sprintf("%s/%s:%s-%d", config.AppBuild.Config.RegistryHost, config.AppBuild.Config.RegistryRepository, appServiceBuildConfig.Name, config.AppBuild.Number)
			err := dockerClient.Build(dockerfileName, []string{tag}, context, buildArgs)
			if err != nil {
				if cleanUpDockerfile {
					fmt.Println("Cleaning up Dockerfile")
					_ = os.Remove(path.Join(context, dockerfileName))
				}
				if cleanUpDockerignore {
					fmt.Println("Cleaning up .dockerignore")
					_ = os.Remove(path.Join(context, dockerignoreName))
				}
				return errors.WithStack(err)
			}
			config.BuiltServices = append(config.BuiltServices, types.BuiltService{
				Name:  appServiceBuildConfig.Name,
				Image: tag,
			})

			if cleanUpDockerfile {
				fmt.Println("Cleaning up dockerfile")
				err = os.Remove(path.Join(context, dockerfileName))
				if err != nil {
					return errors.WithStack(err)
				}
			}
			if cleanUpDockerignore {
				fmt.Println("Cleaning up dockerignore")
				err = os.Remove(path.Join(context, dockerignoreName))
				if err != nil {
					return errors.WithStack(err)
				}
			}
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = os.WriteFile(path.Join(viper.GetString("ci_config_path")), content, 0600)
		if err != nil {
			return errors.WithStack(err)
		}

		return nil
	},
}

func containsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func init() {
	Cmd.Flags().StringVar(&opts.from, "from", ".", "Relative path to codebase")
	Cmd.Flags().StringVar(&opts.to, "to", ".", "Codebase destination path in container")
	Cmd.Flags().StringVarP(&opts.dockerfile, "dockerfile", "f", "", "Relative path to dockerfile")
}
