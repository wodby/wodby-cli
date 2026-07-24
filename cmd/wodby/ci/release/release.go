package release

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
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
	Use:   "release [SERVICE...]",
	Short: "Push images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
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

		logger := log.WithField("stage", "run")
		log.SetOutput(os.Stdout)
		if viper.GetBool("verbose") {
			log.SetLevel(log.DebugLevel)
		}
		if config.BuiltServices == nil {
			return errors.New("No app services have been built to release")
		}

		var servicesToRelease []types.BuiltService
		if len(opts.services) == 0 {
			logger.Info("Releasing all built services")
			servicesToRelease = config.BuiltServices
		} else {
			for _, serviceName := range opts.services {
				found := false
				for _, builtService := range config.BuiltServices {
					if serviceName == builtService.Name {
						found = true
						servicesToRelease = append(servicesToRelease, builtService)
						break
					}
				}
				if !found {
					return errors.New(fmt.Sprintf("No built images found for service %s", serviceName))
				}
			}
		}

		dockerClient := docker.NewClient()
		for _, service := range servicesToRelease {
			logger.Infof("Releasing service %s", service.Name)
			extraTags, err := releaseExtraTags(service.Image, config.AppBuild.GitRefType, config.AppBuild.GitRef, opts.latestBranch, opts.branchTag)
			if err != nil {
				return errors.WithStack(err)
			}
			err = dockerClient.Push(service.Image)
			if err != nil {
				return errors.WithStack(err)
			}
			for _, extraTag := range extraTags {
				err = dockerClient.Tag(service.Image, extraTag)
				if err != nil {
					return errors.WithStack(err)
				}
				err = dockerClient.Push(extraTag)
				if err != nil {
					log.Error("[ERROR] Failed to release image. If you're using Wodby Docker Registry make sure you are within registry storage limits.")
					return errors.WithStack(err)
				}
			}

			for key, svc := range config.BuiltServices {
				if svc.Name == service.Name {
					config.BuiltServices[key].Released = true
					break
				}
			}
		}

		content, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			return errors.WithStack(err)
		}
		err = ioutil.WriteFile(viper.GetString("ci_config_path"), content, 0600)
		if err != nil {
			return errors.WithStack(err)
		}

		return nil
	},
}

func releaseExtraTags(image string, gitRefType types.GitRefType, gitRef string, latestBranch string, branchTag bool) ([]string, error) {
	if gitRefType.Normalize() != types.GitRefTypeBranch {
		return nil, nil
	}

	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return nil, errors.Wrap(err, "invalid image reference")
	}
	base := reference.TrimNamed(named)

	var tags []string
	if gitRef == latestBranch {
		latest, err := reference.WithTag(base, "latest")
		if err != nil {
			return nil, errors.Wrap(err, "failed to create latest image reference")
		}
		tags = append(tags, reference.FamiliarString(latest))
	}
	if branchTag {
		tag, err := dockerTagFromBranch(gitRef)
		if err != nil {
			return nil, err
		}
		branch, err := reference.WithTag(base, tag)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create branch image reference")
		}
		tags = append(tags, reference.FamiliarString(branch))
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
	Cmd.Flags().StringVarP(&opts.latestBranch, "latest-branch", "l", "master", "Update latest tag when built from this branch")
	Cmd.Flags().BoolVarP(&opts.branchTag, "branch-tag", "b", false, "Additionally push a safe tag derived from the current git branch name")
}
