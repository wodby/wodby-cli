package root

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/cmd/wodby/ci"
	"github.com/wodby/wodby-cli/cmd/wodby/ops"
	"github.com/wodby/wodby-cli/cmd/wodby/version"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wodby",
		Short: "CLI client for Wodby 2.0",
	}

	viper.SetEnvPrefix("wodby")
	viper.AutomaticEnv()

	bindPersistentFlags(cmd)
	cmd.AddCommand(ci.Cmd)
	cmd.AddCommand(ops.Commands()...)
	cmd.AddCommand(version.Cmd)

	return cmd
}

func bindPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String("api-key", "", "API key")
	if err := viper.BindPFlag("api_key", cmd.PersistentFlags().Lookup("api-key")); err != nil {
		panic(err)
	}

	cmd.PersistentFlags().String("access-token", "", "Access token")
	if err := viper.BindPFlag("access_token", cmd.PersistentFlags().Lookup("access-token")); err != nil {
		panic(err)
	}

	cmd.PersistentFlags().String("api-endpoint", "https://apiv2.wodby.com/query", "CI API endpoint")
	if err := viper.BindPFlag("api_endpoint", cmd.PersistentFlags().Lookup("api-endpoint")); err != nil {
		panic(err)
	}

	cmd.PersistentFlags().String("api-base-url", "https://api.wodby.com/v1", "Public REST API base URL")
	if err := viper.BindPFlag("api_base_url", cmd.PersistentFlags().Lookup("api-base-url")); err != nil {
		panic(err)
	}

	cmd.PersistentFlags().Bool("verbose", false, "Verbose output")
	if err := viper.BindPFlag("verbose", cmd.PersistentFlags().Lookup("verbose")); err != nil {
		panic(err)
	}

	cmd.PersistentFlags().String("ci-config-path", "/tmp/.wodby-ci.json", "Path to CI config")
	if err := viper.BindPFlag("ci_config_path", cmd.PersistentFlags().Lookup("ci-config-path")); err != nil {
		panic(err)
	}
}
