package release

import (
	"path"
	"fmt"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/pkg/errors"
)

var ciConfig = viper.New()

type options struct {
	services []string
}

var opts options

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "release [service...]",
	Short: "Push images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args

		ciConfig.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))

		err := ciConfig.ReadInConfig()
		if err != nil {
			return err
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		config := new(config.Config)

		err := ciConfig.Unmarshal(config)
		if err != nil {
			return err
		}

		var services []types.Service
		autoRelease := len(opts.services) == 0

		// Validating services for release.
		if autoRelease {
			if config.Stack.Custom {
				return errors.New("You must specify at least one service for release. Auto release is not available for custom stacks")
			} else {
				fmt.Println("Releasing predefined services")
				services = config.Stack.Services
			}
		} else if !config.Stack.Custom && !config.Stack.Fork {
			return errors.New("Releasing specific services is not allowed for managed stacks")
		} else {
			fmt.Println("Validating services")

			for _, svc := range opts.services {
				service, err := config.FindService(svc)

				if err != nil {
					return err
				} else {
					services = append(services, service)
				}
			}
		}

		// Releasing services.
		if len(services) != 0 {
			imagesMap := make(map[string]bool)

			client := docker.NewClient()
			registry := config.Stack.Registry

			err = client.Login(registry.Host, registry.Auth.Username, registry.Auth.Password)
			if err != nil {
				return err
			}

			for _, service := range services {
				// Auto release for managed stacks.
				if autoRelease && service.CI == nil {
					continue
				}

				// Make sure image hasn't been pushed already.
				if _, ok := imagesMap[service.CI.Build.Image]; !ok {
					imagesMap[service.CI.Build.Image] = true
					image := fmt.Sprintf("%s:%s", service.CI.Build.Image, config.Metadata.Number)

					fmt.Println(fmt.Sprintf("Releasing %s image...", service.Name))

					err = client.Push(image)
					if err != nil {
						return err
					}
				}
			}
		} else {
			errors.New("No valid services have been found for release")
		}

		return nil
	},
}
