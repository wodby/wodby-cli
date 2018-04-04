package release

import (
	"path"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"fmt"
)

var ciConfig = viper.New()

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "release.sh",
	Short: "Push images",
	PreRunE: func(cmd *cobra.Command, args []string) error {
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

		client := docker.NewClient()

		imagesMap := make(map[string]bool)
		for _, service := range config.Stack.Services {
			if service.CI == nil {
				continue
			}

			if _, ok := imagesMap[service.CI.Build.Image]; !ok {
				imagesMap[service.CI.Build.Image] = true

				err = client.Login(service.CI.Release.Host, service.CI.Release.Auth.Username, service.CI.Release.Auth.Password)
				if err != nil {
					return err
				}

				fmt.Print(fmt.Sprintf("Pushing %s image to docker registry...", service.Name))
				err = client.Push(service.CI.Build.Image)
				if err != nil {
					return err
				}
				fmt.Println(" DONE")
			}
		}

		return nil
	},
}
