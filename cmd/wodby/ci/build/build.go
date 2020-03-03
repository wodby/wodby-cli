package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
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
	path       string
}

var opts options

const Dockerignore = `.git
.gitignore
.dockerignore`

const DockerfileTpl = `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_FROM
ARG COPY_TO
COPY --chown={{.DefaultUser}}:{{.DefaultUser}} ${COPY_FROM} ${COPY_TO}`

var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "build [OPTIONS] SERVICE... PATH",
	Short: "Build images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.path = args[len(args)-1]
		opts.services = args[:len(args)-1]
		v.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))
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
		if opts.services == nil {
			return errors.New("At least one service must be specified for the build")
		} else {
			logger.Info("Validating services")
			for _, svc := range opts.services {
				found := false
				for _, appServiceBuildConfig := range config.AppBuild.Config.AppServiceBuildConfigs {
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
		if _, err := os.Stat(context + ".dockerignore"); os.IsNotExist(err) {
			err = ioutil.WriteFile(path.Join(context+".dockerignore"), []byte(Dockerignore), 0600)
			if err != nil {
				return errors.WithStack(err)
			}
		}

		dockerClient := docker.NewClient()
		var dockerfile string
		var tag string

		for _, appServiceBuildConfig := range appServiceBuildConfigs {
			buildArgs := make(map[string]string)
			buildArgs["COPY_FROM"] = opts.from
			buildArgs["WODBY_BASE_IMAGE"] = appServiceBuildConfig.Image

			// When user specified custom dockerfile template.
			if opts.dockerfile != "" {
				d, err := ioutil.ReadFile(context + "/" + opts.dockerfile)
				if err != nil {
					return errors.WithStack(err)
				}
				dockerfile = string(d)
			} else {
				if appServiceBuildConfig.Dockerfile != nil {
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
					buildArgs["COPY_TO"] = opts.to
					// Replace default image user in dockerfile template.
					defaultUser, err := dockerClient.GetImageDefaultUser(appServiceBuildConfig.Image)
					if err != nil {
						return errors.WithStack(err)
					}
					t, err := template.New("Dockerfile").Parse(DockerfileTpl)
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

			tag = fmt.Sprintf("%s:%s", appServiceBuildConfig.Slug, config.AppBuild.Number)
			err := dockerClient.Build(dockerfile, []string{tag}, context, buildArgs)
			if err != nil {
				return errors.WithStack(err)
			}
			config.BuiltServices = append(config.BuiltServices, types.BuiltService{
				Name:  appServiceBuildConfig.Name,
				Image: tag,
			})
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = ioutil.WriteFile(path.Join("/tmp/.wodby-ci.json"), content, 0600)
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
