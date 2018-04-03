package deploy

import (
	"log"
	"os"
	"path"

	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"fmt"
)

type commandParams struct {
	UUID       string
	Context    string
	Number     string
	URL        string
	Comment    string
	PostDeploy bool
	Wait       bool
}

var params commandParams

var postDeployFlag *pflag.Flag

var ciConfig = viper.New()

// Cmd represents the deploy command
var Cmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy build to Wodby",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		ciConfig.SetConfigFile(path.Join("/tmp/.wodby-ci.json"))

		err := ciConfig.ReadInConfig()
		if err != nil {
			return err
		}

		params.UUID = ciConfig.GetString("uuid")

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

		apiConfig := &api.Config{
			Key:    ciConfig.GetString("api.key"),
			Scheme: ciConfig.GetString("api.proto"),
			Host:   ciConfig.GetString("api.host"),
			Prefix: ciConfig.GetString("api.prefix"),
		}
		client := api.NewClient(logger, apiConfig)

		var postDeploy *bool
		if postDeployFlag != nil && postDeployFlag.Changed {
			postDeploy = &params.PostDeploy
		}

		if params.Number != "" {
			config.Metadata.Number = params.Number
		}
		if params.URL != "" {
			config.Metadata.URL = params.URL
		}
		if params.Comment != "" {
			config.Metadata.Comment = params.Comment
		}

		payload := &api.DeployBuildPayload{
			Tag:        config.Metadata.Number,
			PostDeploy: postDeploy,
			Metadata:   config.Metadata,
		}

		fmt.Print(fmt.Sprintf("Deploying new build #%s to instance %s...", config.Metadata.Number, config.UUID))
		result, err := client.DeployBuild(params.UUID, payload)
		if err != nil {
			return err
		}

		if params.Wait {
			err := client.WaitTask(result.Task.UUID)
			if err != nil {
				return err
			}
		}
		fmt.Println(" DONE")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&params.Number, "build-number", "", "Build Number")
	Cmd.Flags().StringVar(&params.URL, "build-url", "", "Build URL")
	Cmd.Flags().StringVar(&params.Comment, "comment", "", "Arbitrary message")
	Cmd.Flags().BoolVarP(&params.Wait, "wait", "w", false, "Wait task")
	Cmd.Flags().BoolVar(&params.PostDeploy, "post-deploy", false, "Run post deployment scripts")
	postDeployFlag = Cmd.Flags().Lookup("post-deploy")
}
