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
	context        string
	number         string
	url            string
	tag            string
	skipPostDeploy bool
	services       []string
}

var opts options
var skipPostDeployFlag *pflag.Flag
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

		if len(opts.services) == 0 {
			logger.Info("Deploying all released services")
		}
		servicesToDeploy, err := deploymentServices(config.BuiltServices, opts.services)
		if err != nil {
			return err
		}

		input := types.DeploymentFromCIInput{
			AppBuildID:         config.AppBuild.ID,
			Services:           servicesToDeploy,
			SkipPostDeployment: opts.skipPostDeploy,
		}
		client, err := api.NewClient(config.API)
		if err != nil {
			return errors.WithStack(err)
		}
		deployment, err := client.Deploy(context.Background(), input)
		if err != nil {
			return errors.WithStack(err)
		}
		if deployment.ID == "" {
			return errors.New("Deployment has failed!")
		}

		logger.Infof("Build %d has been queued up for deployment!", config.AppBuild.Number)
		return nil
	},
}

func deploymentServices(builtServices []types.BuiltService, serviceNames []string) ([]*types.ServiceDeploymentInput, error) {
	var servicesToDeploy []*types.ServiceDeploymentInput
	if len(serviceNames) == 0 {
		for _, svc := range builtServices {
			if svc.Released {
				servicesToDeploy = append(servicesToDeploy, &types.ServiceDeploymentInput{
					Name:           svc.Name,
					Image:          svc.Image,
					UnmanagedImage: svc.Unmanaged,
					DockerfilePath: svc.DockerfilePath,
					DockerfileHash: svc.DockerfileHash,
				})
			}
		}

		return servicesToDeploy, nil
	}

	for _, serviceName := range serviceNames {
		found := false
		for _, svc := range builtServices {
			if svc.Name != serviceName {
				continue
			}

			found = true
			if !svc.Released {
				return nil, errors.New(fmt.Sprintf("Service %s hasn't been released", svc.Name))
			}
			servicesToDeploy = append(servicesToDeploy, &types.ServiceDeploymentInput{
				Name:           svc.Name,
				Image:          svc.Image,
				UnmanagedImage: svc.Unmanaged,
				DockerfilePath: svc.DockerfilePath,
				DockerfileHash: svc.DockerfileHash,
			})
			break
		}
		if !found {
			return nil, errors.New(fmt.Sprintf("No built images found for service %s", serviceName))
		}
	}

	return servicesToDeploy, nil
}

func init() {
	Cmd.Flags().BoolVar(&opts.skipPostDeploy, "skip-post-deploy", false, "Skip post deployment scripts execution")
}
