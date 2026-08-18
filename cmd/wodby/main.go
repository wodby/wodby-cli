package main

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/cmd/wodby/root"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

// exitCodeExternalActionRequired reports a paused migration: nothing failed and
// the same command resumes once the external action is done.
const exitCodeExternalActionRequired = 3

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
		// A migration that stopped on a required external action is not a
		// failure. Scripted callers still see a non-zero status, but a distinct
		// one so they can wait and resume instead of paging someone.
		if _, paused := wodby1.AsMigrationPaused(err); paused {
			os.Exit(exitCodeExternalActionRequired)
		}
		os.Exit(1)
	}

	os.Exit(0)
}
