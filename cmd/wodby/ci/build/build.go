package build

import (
	"path"
	"os/exec"

	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"fmt"
	"github.com/wodby/wodby-cli/pkg/config"
	"io/ioutil"
	"os"
)

const Dockerignore = `.git
.gitignore`

var ciConfig = viper.New()

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "build",
	Short: "Build images",
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

		if config.DataContainer != "" {
			from := fmt.Sprintf("%s:/mnt/codebase", config.DataContainer)
			to := fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
			_, err := exec.Command("docker", "cp", from, to).CombinedOutput()
			if err != nil {
				return err
			}
		}

		client := docker.NewClient()

		imagesMap := make(map[string]bool)
		for _, service := range config.Stack.Services {
			if service.CI == nil {
				continue
			}

			if _, ok := imagesMap[service.CI.Build.Image]; !ok {
				imagesMap[service.CI.Build.Image] = true

				var context string
				if config.DataContainer != "" {
					context = fmt.Sprintf("/tmp/wodby-build-%s", config.DataContainer)
				} else {
					context = ciConfig.GetString("context")
				}

				if _, err := os.Stat(context + ".dockerignore"); os.IsNotExist(err) {
					err = ioutil.WriteFile(path.Join(context + ".dockerignore"), []byte(Dockerignore), 0600)
					if err != nil {
						return err
					}
				}

				image := fmt.Sprintf("%s:%s", service.CI.Build.Image, config.Metadata.Number)
				fmt.Print(fmt.Sprintf("Building %s image", service.Name))
				err := client.Build(service.CI.Build.Dockerfile, image, context)
				if err != nil {
					return err
				}
				fmt.Println(" DONE")
			}
		}

		return nil
	},
}
