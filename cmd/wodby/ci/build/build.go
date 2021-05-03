package build

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/pkg/errors"
)

type options struct {
	from       string
	to         string
	dockerfile string
	tag        string
	services   []string
}

type imageBuild struct {
	dockerfile   string
	buildArgs    map[string]string
	tags         []string
	serviceNames []string
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

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "build [service...]",
	Short: "Build images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args

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

		services := make(map[string]types.Service)

		if len(opts.services) == 0 {
			fmt.Println("Building all services")
			services = config.BuildConfig.Services
		} else {
			fmt.Println("Validating services")

			for _, svc := range opts.services {
				// Find services by prefix.
				if svc[len(svc)-1] == '-' {
					matchingServices, err := config.FindServicesByPrefix(svc)

					if err != nil {
						return err
					}

					for _, service := range matchingServices {
						fmt.Printf("Found matching service %s\n", service.Name)
						services[service.Name] = service
					}
				} else {
					service, err := config.FindService(svc)

					if err != nil {
						return err
					}

					services[service.Name] = service
				}
			}
		}

		if len(services) == 0 {
			return errors.New("No valid services have been found for build")
		}

		if config.DataContainer != "" {
			fmt.Println("Synchronizing data container")

			from := fmt.Sprintf("%s:%s", config.DataContainer, config.WorkingDir)
			to := fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
			output, err := exec.Command("docker", "cp", from, to).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}
		}

		var context string
		if config.DataContainer != "" {
			context = fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
		} else {
			context = v.GetString("context")
		}

		if _, err := os.Stat(context + ".dockerignore"); os.IsNotExist(err) {
			err = ioutil.WriteFile(path.Join(context+".dockerignore"), []byte(Dockerignore), 0600)
			if err != nil {
				return err
			}
		}

		dockerClient := docker.NewClient()

		var (
			dockerfile string
			tpl        string
			tag        string
		)

		imageBuilds := make(map[string]*imageBuild)

		// Prepare image builds.
		for _, service := range services {
			buildArgs := make(map[string]string)
			buildArgs["COPY_FROM"] = opts.from
			buildArgs["COPY_TO"] = opts.to
			buildArgs["WODBY_BASE_IMAGE"] = service.Image

			// When user specified custom dockerfile template.
			if opts.dockerfile != "" {
				d, err := ioutil.ReadFile(context + "/" + opts.dockerfile)

				if err != nil {
					return err
				}

				tpl = string(d)
			} else {
				tpl = DockerfileTpl
			}

			// Replace default image user in dockerfile template.
			defaultUser, err := dockerClient.GetImageDefaultUser(service.Image)

			if err != nil {
				return err
			}

			t, err := template.New("Dockerfile").Parse(tpl)
			if err != nil {
				return err
			}

			data := struct{ DefaultUser string }{DefaultUser: defaultUser}
			var tpl bytes.Buffer

			if err := t.Execute(&tpl, data); err != nil {
				return err
			}

			dockerfile = tpl.String()

			// Allow specifying tags for custom stacks.
			if opts.tag != "" {
				if strings.Contains(opts.tag, ":") {
					tag = opts.tag
				} else {
					tag = fmt.Sprintf("%s:%s", opts.tag, config.Metadata.Number)
				}
			} else {
				tag = fmt.Sprintf("%s:%s", service.Slug, config.Metadata.Number)
			}

			// Group equal builds in one build with multiple tags.
			if _, ok := imageBuilds[service.Image]; ok {
				imageBuilds[service.Image].tags = append(imageBuilds[service.Image].tags, tag)
				imageBuilds[service.Image].serviceNames = append(imageBuilds[service.Image].serviceNames, service.Name)
			} else {
				build := &imageBuild{
					dockerfile:   dockerfile,
					buildArgs:    buildArgs,
					tags:         []string{tag},
					serviceNames: []string{service.Name},
				}

				imageBuilds[service.Image] = build
			}
		}

		// Building images.
		for _, build := range imageBuilds {
			fmt.Printf("Building image for service(s) %s...\n", strings.Join(build.serviceNames, ", "))
			err := dockerClient.Build(build.dockerfile, build.tags, context, build.buildArgs)

			if err != nil {
				return err
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.from, "from", ".", "Relative path to codebase")
	Cmd.Flags().StringVar(&opts.to, "to", ".", "Codebase destination path in container")
	Cmd.Flags().StringVarP(&opts.dockerfile, "dockerfile", "f", "", "Relative path to dockerfile")
	Cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format. Use if you want to use custom docker registry")
}
