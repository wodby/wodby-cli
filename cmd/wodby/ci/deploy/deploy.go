package deploy

import (
	"log"
	"os"
	"path"
	"fmt"
	"strings"

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
var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "deploy [service...]",
	Short: "Deploy build to Wodby",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		opts.services = args

		v.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))

		err := v.ReadInConfig()
		if err != nil {
			return err
		}

		opts.uuid = v.GetString("uuid")

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var logger *log.Logger

		if viper.GetBool("verbose") == true {
			logger = log.New(os.Stdout, "", log.LstdFlags)
		}

		config := new(config.Config)

		err := v.Unmarshal(config)
		if err != nil {
			return err
		}

		services := make(map[string]types.Service)

		if len(opts.services) == 0 {
			fmt.Println("Deploying all services")
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
			return errors.New("No valid services have been found for deploy")
		}

		// Deploying services.
		apiConfig := &api.Config{
			Key:    v.GetString("api.key"),
			Scheme: v.GetString("api.proto"),
			Host:   v.GetString("api.host"),
			Prefix: v.GetString("api.prefix"),
		}
		docker := api.NewClient(logger, apiConfig)

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

		var tag string

		// Allow specifying tags for custom stacks.
		if opts.tag != "" {
			if !config.Stack.Custom {
				return errors.New("Specifying tags not allowed for managed stacks")
			}

			if strings.Contains(opts.tag, ":") {
				tag = opts.tag
			} else {
				tag = fmt.Sprintf("%s:%s", opts.tag, config.Metadata.Number)
			}

			for _, service := range services {
				payload.ServicesTags[service.Name] = tag
			}
		}

		fmt.Printf("Deploying build #%s to %s...", config.Metadata.Number, config.Stack.Title)
		result, err := docker.DeployBuild(opts.uuid, payload)
		if err != nil {
			return err
		}

		err = docker.WaitTask(result.Task.UUID)
		if err != nil {
			return err
		}

		fmt.Println(" DONE")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.number, "build-number", "", "Build number")
	Cmd.Flags().StringVar(&opts.url, "build-url", "", "Build url")
	Cmd.Flags().StringVar(&opts.comment, "comment", "", "Arbitrary message")
	Cmd.Flags().StringVarP(&opts.tag, "tag", "t", "", "Name and optionally a tag in the 'name:tag' format. Use if you want to use custom docker registry")
	Cmd.Flags().BoolVar(&opts.postDeploy, "post-deploy", false, "Run post deployment scripts")
	postDeployFlag = Cmd.Flags().Lookup("post-deploy")
}
