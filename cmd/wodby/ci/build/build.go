package build

import (
	"path"
	"os"
	"os/exec"
	"fmt"
	"io/ioutil"
	"html/template"
	"bytes"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/pkg/errors"
)

type options struct {
	fixPermissions	bool
	from       		string
	to         		string
	dockerfile 		string
	services 		[]string
}

var opts options

const Dockerignore = `.git
.gitignore
.dockerignore`

const Dockerfile = `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_FROM
ARG COPY_TO
COPY --chown={{.User}}:{{.User}} ${COPY_FROM} ${COPY_TO}`

var v = viper.New()

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "build [service...]",
	Short: "Build images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args

		v.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))

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

		var services map[string]types.Service
		var dockerfile string
		buildArgs := make(map[string]string)
		autoBuild := len(opts.services) == 0

		// Validating services.
		if autoBuild {
			if config.Stack.Custom {
				return errors.New("You must specify at least one service for build. Auto build is not available for custom stacks")
			} else {
				fmt.Println("Building predefined services")
				services = config.Stack.Services
			}
		} else if !config.Stack.Custom && !config.Stack.Fork {
			return errors.New("Building specific services is not allowed for managed stacks")
		} else {
			fmt.Println("Validating services")

			for _, svc := range opts.services {
				service, err := config.FindService(svc)

				if err != nil {
					return err
				} else {
					services[service.Name] = service
				}
			}
		}

		// Building services.
		if len(services) != 0 {
			if config.DataContainer != "" {
				from := fmt.Sprintf("%s:/mnt/codebase", config.DataContainer)
				to := fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
				_, err := exec.Command("docker", "cp", from, to).CombinedOutput()
				if err != nil {
					return err
				}
			}

			docker := docker.NewClient()

			var context string
			if config.DataContainer != "" {
				context = fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
			} else {
				context = v.GetString("context")
			}

			if _, err := os.Stat(context + ".dockerignore"); os.IsNotExist(err) {
				err = ioutil.WriteFile(path.Join(context + ".dockerignore"), []byte(Dockerignore), 0600)
				if err != nil {
					return err
				}
			}

			imagesMap := make(map[string]bool)

			for _, service := range services {
				// Auto build for managed stacks.
				if autoBuild {
					if service.CI == nil {
						continue
					}

					dockerfile = service.CI.Build.Dockerfile
				// Configurable build for custom stacks.
				} else {
					buildArgs["WODBY_BASE_IMAGE"] = service.Image

					if opts.dockerfile != "" {
						d, err := ioutil.ReadFile(context + "/" + opts.dockerfile)

						if err != nil {
							return err
						}

						dockerfile = string(d)

					} else if opts.from != "" && opts.to != "" {
						buildArgs["COPY_FROM"] = opts.from
						buildArgs["COPY_TO"] = opts.to

						// Define and set default user in dockerfile.
						defaultUser, err := docker.GetDefaultImageUser(service.Image)

						if err != nil {
							return err
						}

						t, err := template.New("Dockerfile").Parse(Dockerfile)
						if err != nil {
							return err
						}

						data := struct{User string}{User: defaultUser}
						var tpl bytes.Buffer

						if err := t.Execute(&tpl, data); err != nil {
							return err
						}

						dockerfile = tpl.String()

					} else {
						return errors.New("Missing mandatory flags for service build: --dockerfile or --from --to")
					}
				}

				// Make sure image hasn't been built already.
				if _, ok := imagesMap[service.CI.Build.Image]; !ok {
					imagesMap[service.CI.Build.Image] = true
					image := fmt.Sprintf("%s:%s", service.CI.Build.Image, config.Metadata.Number)

					fmt.Println(fmt.Sprintf("Building %s image...", service.Name))
					err := docker.Build(dockerfile, image, context, buildArgs)

					if err != nil {
						return err
					}
				}
			}
		} else {
			errors.New("No valid services have been found for build")
		}

		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&opts.fixPermissions, "fix-permissions", false, "Fix ownership of copied codebase to image default user")
	Cmd.Flags().StringVarP(&opts.from, "from", "f", ".", "relative path to codebase")
	Cmd.Flags().StringVarP(&opts.to, "to", "t", "", "codebase destination path in container")
	Cmd.Flags().StringVarP(&opts.dockerfile, "dockerfile", "d", "", "relative path to dockerfile")
}
