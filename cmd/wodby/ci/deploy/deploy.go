package deploy

import (
	"log"
	"os"
	"path"
	"fmt"

	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/pkg/errors"
)

type options struct {
	uuid       string
	context    string
	number     string
	url        string
	comment    string
	tag        string
	postDeploy bool
	services   []string
}

var opts options
var postDeployFlag *pflag.Flag
var ciConfig = viper.New()

var Cmd = &cobra.Command{
	Use:   "deploy [service...]",
	Short: "Deploy build to Wodby",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args

		ciConfig.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))

		err := ciConfig.ReadInConfig()
		if err != nil {
			return err
		}

		opts.uuid = ciConfig.GetString("uuid")

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var logger *log.Logger

		if viper.GetBool("verbose") == true {
			logger = log.New(os.Stdout, "", log.LstdFlags)
		}

		config := new(config.Config)

		err := ciConfig.Unmarshal(config)
		if err != nil {
			return err
		}

		var services []types.Service
		autoDeploy := len(opts.services) == 0

		// Validating services for deploy.
		if autoDeploy {
			if config.Stack.Custom {
				return errors.New("You must specify at least one service for deployment. Auto deploy is not available for custom stacks")
			} else {
				fmt.Println("Releasing predefined services")
			}
		} else if !config.Stack.Custom && !config.Stack.Fork {
			return errors.New("Deploying specific services is not allowed for managed stacks")
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

		// Deploying services.
		if len(services) != 0 || autoDeploy {
			apiConfig := &api.Config{
				Key:    ciConfig.GetString("api.key"),
				Scheme: ciConfig.GetString("api.proto"),
				Host:   ciConfig.GetString("api.host"),
				Prefix: ciConfig.GetString("api.prefix"),
			}
			client := api.NewClient(logger, apiConfig)

			var postDeploy *bool
			if postDeployFlag != nil && postDeployFlag.Changed {
				postDeploy = &opts.postDeploy
			}

			if opts.number != "" {
				config.Metadata.Number = opts.number
			}
			if opts.url != "" {
				config.Metadata.URL = opts.url
			}
			if opts.comment != "" {
				config.Metadata.Comment = opts.comment
			}

			payload := &api.DeployBuildPayload{
				Number:     config.Metadata.Number,
				PostDeploy: postDeploy,
				Metadata:   config.Metadata,
			}

			servicesTags := make(map[string]string)

			if !autoDeploy {
				for _, service := range services {
					if opts.tag != "" {
						servicesTags[service.Name] = opts.tag
					} else {
						servicesTags[service.Name] = config.Metadata.Number
					}
				}

				payload.ServicesTags = servicesTags
			}

			fmt.Print(fmt.Sprintf("Deploying build #%s to %s...", config.Metadata.Number, config.Stack.Instance.Title))
			result, err := client.DeployBuild(opts.uuid, payload)
			if err != nil {
				return err
			}

			err = client.WaitTask(result.Task.UUID)
			if err != nil {
				return err
			}

			fmt.Println(" DONE")
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.number, "build-number", "", "Build number")
	Cmd.Flags().StringVar(&opts.url, "build-url", "", "Build url")
	Cmd.Flags().StringVar(&opts.comment, "comment", "", "Arbitrary message")
	Cmd.Flags().StringVar(&opts.tag, "image-tag", "", "Image tag when deploying from personal container registry")
	Cmd.Flags().BoolVar(&opts.postDeploy, "post-deploy", false, "Run post deployment scripts")
	postDeployFlag = Cmd.Flags().Lookup("post-deploy")
}
