package deploy

import (
	"context"
	"fmt"
	"os"
	"path"

	log "github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/types"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/pkg/errors"
)

type options struct {
	context    string
	number     string
	url        string
	tag        string
	postDeploy bool
	services   []string
}

var opts options
var postDeployFlag *pflag.Flag
var v = viper.New()

var Cmd = &cobra.Command{
	Use:   "deploy [SERVICE...]",
	Short: "Deploy build to Wodby",
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

		logger := log.WithField("stage", "deploy")
		log.SetOutput(os.Stdout)
		if viper.GetBool("verbose") {
			log.SetLevel(log.DebugLevel)
		}

		if config.BuiltServices == nil {
			return errors.New("No app services have been built to deploy")
		}
		released := false
		for _, svc := range config.BuiltServices {
			if svc.Released {
				released = true
				break
			}
		}
		if !released {
			return errors.New("No app services have been released to deploy")
		}

		var servicesToDeploy []*types.ServiceDeploymentInput
		if len(opts.services) == 0 {
			logger.Info("Deploying all released services")
			for _, svc := range config.BuiltServices {
				if svc.Released {
					servicesToDeploy = append(servicesToDeploy, &types.ServiceDeploymentInput{
						Name:  svc.Name,
						Image: svc.Image,
					})
				}
			}
		} else {
			for _, serviceName := range opts.services {
				for _, svc := range config.BuiltServices {
					if svc.Name == serviceName {
						if !svc.Released {
							return errors.New(fmt.Sprintf("Service %s hasn't been released", svc.Name))
						}
						servicesToDeploy = append(servicesToDeploy, &types.ServiceDeploymentInput{
							Name:  svc.Name,
							Image: svc.Image,
						})
						break
					}
				}
			}
		}

		var postDeploy bool
		if postDeployFlag != nil && postDeployFlag.Changed {
			postDeploy = opts.postDeploy
		}
		input := types.DeploymentInput{
			AppBuildID:     config.AppBuild.ID,
			Services:       servicesToDeploy,
			PostDeployment: postDeploy,
		}
		client := api.NewClient(config.API)
		deployment, err := client.Deploy(context.Background(), input)
		if err != nil {
			return errors.WithStack(err)
		}
		if deployment.ID == 0 {
			return errors.New("Deployment has failed!")
		}

		logger.Infof("Build %d has been queued up for deployment!", config.AppBuild.Number)
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&opts.postDeploy, "post-deploy", true, "Run post deployment scripts")
	postDeployFlag = Cmd.Flags().Lookup("post-deploy")
}
