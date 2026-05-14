package release

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	log "github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"regexp"

	"github.com/pkg/errors"
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
			err = dockerClient.Push(service.Image)
			if err != nil {
				return errors.WithStack(err)
			}

			extraTags, err := releaseExtraTags(service.Image, config.AppBuild.GitRefType, config.AppBuild.GitRef, opts.latestBranch, opts.branchTag)
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

	r, err := regexp.Compile(":.+$")
	if err != nil {
		return nil, err
	}

	var tags []string
	if gitRef == latestBranch {
		tags = append(tags, r.ReplaceAllString(image, ":latest"))
	}
	if branchTag {
		tags = append(tags, r.ReplaceAllString(image, ":"+gitRef))
	}

	return tags, nil
}

func init() {
	Cmd.Flags().StringVarP(&opts.latestBranch, "latest-branch", "l", "master", "Update latest tag when built from this branch")
	Cmd.Flags().BoolVarP(&opts.branchTag, "branch-tag", "b", false, "Additionally push tag with the current git branch name")
}
