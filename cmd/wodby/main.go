package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/cmd/wodby/root"
)

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel != "" {
		level, err := logrus.ParseLevel(logLevel)
		if err != nil {
			panic(err)
		}
		logrus.SetLevel(level)
	}

	if err := root.NewCommand().Execute(); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}
