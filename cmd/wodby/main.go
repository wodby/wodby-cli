package main

import (
	"os"

	"github.com/wodby/wodby-cli/cmd/wodby/ci"
	"github.com/wodby/wodby-cli/cmd/wodby/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// RootCmd represents the base command when called without any subcommands.
var RootCmd = &cobra.Command{
	Use:   "wodby",
	Short: "CLI client for Wodby",
}

func init() {
	viper.SetEnvPrefix("wodby")
	viper.AutomaticEnv()

	RootCmd.PersistentFlags().String("api-key", "", "API key")
	viper.BindPFlag("api_key", RootCmd.PersistentFlags().Lookup("api-key"))

	RootCmd.PersistentFlags().String("api-proto", "https", "API protocol")
	viper.BindPFlag("api_proto", RootCmd.PersistentFlags().Lookup("api-proto"))

	RootCmd.PersistentFlags().String("api-host", "api.wodby.com", "API host")
	viper.BindPFlag("api_host", RootCmd.PersistentFlags().Lookup("api-host"))

	RootCmd.PersistentFlags().String("api-prefix", "api/v2", "API prefix")
	viper.BindPFlag("api_prefix", RootCmd.PersistentFlags().Lookup("api-prefix"))

	RootCmd.PersistentFlags().Bool("verbose", false, "Verbose output")
	viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))

	RootCmd.PersistentFlags().Bool("dump", false, "Dump API responses")
	RootCmd.PersistentFlags().MarkHidden("dump")
	viper.BindPFlag("dump", RootCmd.PersistentFlags().Lookup("dump"))

	RootCmd.AddCommand(ci.Cmd)
	RootCmd.AddCommand(version.Cmd)
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}
