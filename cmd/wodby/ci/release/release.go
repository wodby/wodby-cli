package release

import (
	"crypto/sha256"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
	"github.com/wodby/wodby-cli/pkg/utils"
)

const dockerTagMaxLength = 128

var (
	invalidDockerTagChars  = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	tagValidationReference = func() reference.Named {
		named, err := reference.WithName("wodby/validation")
		if err != nil {
			panic(err)
		}
		return named
	}()
)

var v = viper.New()

type options struct {
	tag          string
	services     []string
	latestBranch string
	branchTag    bool
}

var opts options

var Cmd = &cobra.Command{
	Use:   "release [service...]",
	Short: "Push images",
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
			fmt.Println("Releasing all services")
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
					} else {
						services[service.Name] = service
					}
				}
			}
		}

		if len(services) == 0 {
			return errors.New("No valid services have been found for release")
		}

		// Releasing services.
		imagesMap := make(map[string]bool)

		docker := docker.NewClient()
		registry := config.BuildConfig.Registry

		if opts.tag == "" {
			err = docker.Login(registry.Host, registry.Username, registry.Password)
			if err != nil {
				return err
			}
		}

		for _, service := range services {
			// Make sure image hasn't been pushed already.
			if _, ok := imagesMap[service.Slug]; !ok {
				imagesMap[service.Slug] = true

				var tag string

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

				extraTags, err := releaseExtraTags(tag, config.Metadata.Branch, opts.latestBranch, opts.branchTag)
				if err != nil {
					return err
				}

				fmt.Printf("Pushing %s image...", service.Name)
				err = docker.Push(tag)
				if err != nil {
					return err
				}

				for _, extraTag := range extraTags {
					err = docker.Tag(tag, extraTag)
					if err != nil {
						return err
					}
					err = docker.Push(extraTag)
					if err != nil {
						return err
					}
				}
			}
		}

		return nil
	},
}

func releaseExtraTags(image string, branch string, latestBranch string, branchTag bool) ([]string, error) {
	if branch == "" {
		return nil, nil
	}

	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nil, errors.Wrap(err, "invalid image reference")
	}
	base := reference.TrimNamed(named)

	var tags []string
	if branch == latestBranch {
		latest, err := reference.WithTag(base, "latest")
		if err != nil {
			return nil, errors.Wrap(err, "failed to create latest image reference")
		}
		tags = append(tags, reference.FamiliarString(latest))
	}
	if branchTag {
		tag, err := dockerTagFromBranch(branch)
		if err != nil {
			return nil, err
		}
		branchImage, err := reference.WithTag(base, tag)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create branch image reference")
		}
		tags = append(tags, reference.FamiliarString(branchImage))
	}

	return tags, nil
}

func dockerTagFromBranch(branch string) (string, error) {
	if branch == "" {
		return "", errors.New("cannot create an image tag from an empty git branch")
	}

	if len(branch) <= dockerTagMaxLength {
		if _, err := reference.WithTag(tagValidationReference, branch); err == nil {
			return branch, nil
		}
	}

	base := invalidDockerTagChars.ReplaceAllString(branch, "-")
	base = strings.TrimLeft(base, ".-")
	if base == "" {
		base = "branch"
	}

	hash := sha256.Sum256([]byte(branch))
	suffix := fmt.Sprintf("-%x", hash[:4])
	maxBaseLength := dockerTagMaxLength - len(suffix)
	if len(base) > maxBaseLength {
		base = base[:maxBaseLength]
	}
	base = strings.TrimRight(base, ".-")
	if base == "" {
		base = "branch"
	}
	tag := base + suffix

	if _, err := reference.WithTag(tagValidationReference, tag); err != nil {
		return "", errors.Wrap(err, "failed to create a valid image tag from git branch")
	}
	return tag, nil
}

func init() {
	Cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format. Use if you want to use custom docker registry")
	Cmd.Flags().StringVarP(&opts.latestBranch, "latest-branch", "l", "master", "Update latest tag when built from this branch")
	Cmd.Flags().BoolVarP(&opts.branchTag, "branch-tag", "b", false, "Additionally push a safe tag derived from the current git branch name")
}
