package build

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/cicache"
	"github.com/wodby/wodby-cli/pkg/utils"

	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/pkg/errors"
)

type options struct {
	from            string
	to              string
	dockerfile      string
	tag             string
	services        []string
	buildArgs       []string
	buildArgEnvVars []string
}

type imageBuild struct {
	dockerfile   string
	buildArgs    map[string]string
	redactions   []string
	tags         []string
	serviceNames []string
}

var opts options

const Dockerignore = `.git
.gitignore
.dockerignore
.wodby-ci-cache`

const DockerfileTpl = `ARG WODBY_BASE_IMAGE
FROM ${WODBY_BASE_IMAGE}
ARG COPY_FROM
ARG COPY_TO
COPY --chown={{.DefaultUserOwnership}} ${COPY_FROM} ${COPY_TO}`

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

			context, err := prepareDataContainerContext(config.DataContainer)
			if err != nil {
				return err
			}
			defer os.RemoveAll(context)

			from := fmt.Sprintf("%s:%s", config.DataContainer, dataContainerWorkingDirContents(config.WorkingDir))
			output, err := exec.Command("docker", "cp", from, context).CombinedOutput()
			if err != nil {
				return errors.Wrap(err, string(output))
			}
		}

		var context string
		if config.DataContainer != "" {
			context = dataContainerContextPath(config.DataContainer)
		} else {
			context = v.GetString("context")
		}

		cleanupDockerignore, err := ensureDefaultDockerignore(context)
		if err != nil {
			return err
		}
		defer cleanupDockerignore()

		dockerClient := docker.NewClient()

		var (
			dockerfile string
			tpl        string
			tag        string
		)

		imageBuilds := make(map[string]*imageBuild)

		// Prepare image builds.
		for _, service := range orderedServices(services, config.BuildConfig.Default) {
			buildArgs := make(map[string]string)
			var redactions []string
			buildArgs["COPY_FROM"] = opts.from
			buildArgs["COPY_TO"] = opts.to
			buildArgs["WODBY_BASE_IMAGE"] = service.Image

			for _, buildArg := range opts.buildArgs {
				name, value, err := parseBuildArg(buildArg)
				if err != nil {
					return err
				}

				buildArgs[name] = value
			}

			for _, envName := range opts.buildArgEnvVars {
				value, ok := os.LookupEnv(envName)
				if !ok {
					return errors.Errorf("environment variable %s is not set", envName)
				}

				// Forward the variable by name so its value is not exposed in the
				// process arguments or in the displayed Docker command.
				buildArgs[envName] = ""
				redactions = append(redactions, value)
			}

			// When user specified custom dockerfile template.
			if opts.dockerfile != "" {
				d, err := os.ReadFile(filepath.Join(context, opts.dockerfile))

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

			dockerfile, err = renderDockerfileTemplate(tpl, defaultUser)
			if err != nil {
				return err
			}

			// Allow specifying tags for custom stacks.
			if opts.tag != "" {
				if strings.Contains(opts.tag, ":") {
					tag = opts.tag
				} else {
					tag = utils.BuildTag(opts.tag, service.Slug, config.Metadata.Number)
				}
			} else {
				tag = fmt.Sprintf("%s:%s", service.Slug, config.Metadata.Number)
			}

			// Group equal builds in one build with multiple tags.
			if _, ok := imageBuilds[service.Image]; ok {
				imageBuilds[service.Image].tags = append(imageBuilds[service.Image].tags, tag)
				imageBuilds[service.Image].serviceNames = append(imageBuilds[service.Image].serviceNames, service.Name)
				imageBuilds[service.Image].redactions = append(imageBuilds[service.Image].redactions, redactions...)
			} else {
				build := &imageBuild{
					dockerfile:   dockerfile,
					buildArgs:    buildArgs,
					redactions:   redactions,
					tags:         []string{tag},
					serviceNames: []string{service.Name},
				}

				imageBuilds[service.Image] = build
			}
		}

		// Building images.
		for _, image := range orderedBuildImages(imageBuilds, services[config.BuildConfig.Default].Image) {
			build := imageBuilds[image]
			fmt.Printf("Building image for service(s) %s...\n", strings.Join(build.serviceNames, ", "))
			err := dockerClient.BuildWithRedactions(build.dockerfile, build.tags, context, build.buildArgs, build.redactions)

			if err != nil {
				return err
			}
		}

		return nil
	},
}

// renderDockerfileTemplate keeps the legacy DefaultUser variable available to
// custom Dockerfiles while exposing ownership that preserves explicit groups.
func renderDockerfileTemplate(dockerfile string, defaultUser string) (string, error) {
	t, err := template.New("Dockerfile").Parse(dockerfile)
	if err != nil {
		return "", err
	}

	data := struct {
		DefaultUser          string
		DefaultUserOwnership string
	}{
		DefaultUser:          defaultUser,
		DefaultUserOwnership: docker.ChownSpec(defaultUser),
	}
	var rendered bytes.Buffer

	if err := t.Execute(&rendered, data); err != nil {
		return "", err
	}

	return rendered.String(), nil
}

func dataContainerContextPath(dataContainer string) string {
	return filepath.Join(os.TempDir(), "wodby-build-"+dataContainer)
}

func prepareDataContainerContext(dataContainer string) (string, error) {
	if dataContainer == "" || filepath.Base(dataContainer) != dataContainer {
		return "", errors.Errorf("invalid data container name %q", dataContainer)
	}

	context := dataContainerContextPath(dataContainer)
	if err := os.RemoveAll(context); err != nil {
		return "", err
	}
	return context, nil
}

func dataContainerWorkingDirContents(workingDir string) string {
	cleaned := path.Clean(workingDir)
	if cleaned == "." || cleaned == "/" {
		return "/."
	}

	return cleaned + "/."
}

func ensureDefaultDockerignore(context string) (func(), error) {
	dockerignorePath := filepath.Join(context, ".dockerignore")
	original, err := os.ReadFile(dockerignorePath)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	content := string(original)
	mode := os.FileMode(0600)
	if existed {
		info, err := os.Stat(dockerignorePath)
		if err != nil {
			return nil, err
		}
		mode = info.Mode().Perm()
	} else {
		content = Dockerignore
	}

	if !dockerignoreContains(content, cicache.DirectoryName) {
		content = strings.TrimRight(content, "\n") + "\n" + cicache.DirectoryName + "\n"
	}
	if existed && content == string(original) {
		return func() {}, nil
	}

	if err := os.WriteFile(dockerignorePath, []byte(content), mode); err != nil {
		return nil, err
	}

	return func() {
		if existed {
			_ = os.WriteFile(dockerignorePath, original, mode)
		} else {
			_ = os.Remove(dockerignorePath)
		}
	}, nil
}

func dockerignoreContains(content, entry string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}
	return false
}

func orderedServices(services map[string]types.Service, defaultService string) []types.Service {
	names := make([]string, 0, len(services))
	for name := range services {
		if name != defaultService {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	ordered := make([]types.Service, 0, len(services))
	if service, ok := services[defaultService]; ok {
		ordered = append(ordered, service)
	}
	for _, name := range names {
		ordered = append(ordered, services[name])
	}
	return ordered
}

func orderedBuildImages(builds map[string]*imageBuild, defaultImage string) []string {
	images := make([]string, 0, len(builds))
	for image := range builds {
		if image != defaultImage {
			images = append(images, image)
		}
	}
	sort.Strings(images)

	if _, ok := builds[defaultImage]; ok {
		images = append([]string{defaultImage}, images...)
	}
	return images
}

func parseBuildArg(raw string) (string, string, error) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", errors.Errorf("invalid build arg %q, expected NAME=VALUE", raw)
	}

	return parts[0], parts[1], nil
}

func init() {
	Cmd.Flags().StringVar(&opts.from, "from", ".", "Relative path to codebase")
	Cmd.Flags().StringVar(&opts.to, "to", ".", "Codebase destination path in container")
	Cmd.Flags().StringVarP(&opts.dockerfile, "dockerfile", "f", "", "Relative path to dockerfile")
	Cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format. Use if you want to use custom docker registry")
	Cmd.Flags().StringArrayVar(&opts.buildArgs, "build-arg", nil, "Additional build argument in the 'NAME=VALUE' format. Repeatable")
	Cmd.Flags().StringArrayVar(&opts.buildArgEnvVars, "build-arg-env", nil, "Environment variable name to forward as a docker build argument. Repeatable")
}
