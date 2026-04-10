package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/cmd/wodby/ci"
	"github.com/wodby/wodby-cli/cmd/wodby/version"
)

var RootCmd = &cobra.Command{
	Use:   "wodby",
	Short: "CLI client for Wodby 2.0",
}

func init() {
	viper.SetEnvPrefix("wodby")
	viper.AutomaticEnv()

	RootCmd.PersistentFlags().String("api-key", "", "API key")
	err := viper.BindPFlag("api_key", RootCmd.PersistentFlags().Lookup("api-key"))
	if err != nil {
		panic(err)
	}

	RootCmd.PersistentFlags().String("api-endpoint", "https://apiv2.wodby.com/query", "API endpoint")
	err = viper.BindPFlag("api_endpoint", RootCmd.PersistentFlags().Lookup("api-endpoint"))
	if err != nil {
		panic(err)
	}

	RootCmd.PersistentFlags().Bool("verbose", false, "Verbose output")
	err = viper.BindPFlag("verbose", RootCmd.PersistentFlags().Lookup("verbose"))
	if err != nil {
		panic(err)
	}

	RootCmd.PersistentFlags().String("ci-config-path", "/tmp/.wodby-ci.json", "Path to CI config")
	err = viper.BindPFlag("ci_config_path", RootCmd.PersistentFlags().Lookup("ci-config-path"))
	if err != nil {
		panic(err)
	}

	RootCmd.AddCommand(ci.Cmd)
	RootCmd.AddCommand(version.Cmd)
}

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel != "" {
		level, err := logrus.ParseLevel(logLevel)
		if err != nil {
			panic(err)
		}
		logrus.SetLevel(level)
	}
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}
