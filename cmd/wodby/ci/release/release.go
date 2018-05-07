package release

import (
	"path"
	"fmt"
	"strings"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/pkg/errors"
)

var v = viper.New()

type options struct {
	tag 	 		string
	services 		[]string
	latestBranch 	string
	branchTag 		bool
}

var opts options

var Cmd = &cobra.Command{
	Use:   "release [service...]",
	Short: "Push images",
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

		services := make(map[string]types.Service)

		if len(opts.services) == 0 {
			fmt.Println("Releasing all services")
			services = config.Stack.Services
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
		registry := config.Stack.Registry

		err = docker.Login(registry.Host, registry.Username, registry.Password)
		if err != nil {
			return err
		}

		for _, service := range services {
			// Make sure image hasn't been pushed already.
			if _, ok := imagesMap[service.Slug]; !ok {
				imagesMap[service.Slug] = true

				var tag string

				// Allow specifying tags for custom stacks.
				if opts.tag != "" {
					if config.Stack.Custom {
						return errors.New("Specifying tags not allowed for managed stacks")
					}

					if strings.Contains(opts.tag, ":") {
						tag = opts.tag
					} else {
						tag = fmt.Sprintf("%s:%s", opts.tag, config.Metadata.Number)
					}
				} else {
					tag = fmt.Sprintf("%s:%s", service.Slug, config.Metadata.Number)
				}

				fmt.Printf("Pushing %s image...", service.Name)

				err = docker.Push(tag)
				if err != nil {
					return err
				}

				if config.Metadata.Branch != "" {
					if config.Metadata.Branch == opts.latestBranch {
						err = docker.Push(service.Slug + ":latest")
						if err != nil {
							return err
						}
					}

					if opts.branchTag {
						err = docker.Push(service.Slug + ":" + config.Metadata.Branch)
						if err != nil {
							return err
						}
					}
				}
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format. Use if you want to use custom docker registry")
	Cmd.Flags().StringVarP(&opts.latestBranch, "latest-branch", "l", "master", "Update latest tag when built from this branch")
	Cmd.Flags().BoolVarP(&opts.branchTag, "branch-tag","b", false, "Additionally push tag with the current git branch name")
}
