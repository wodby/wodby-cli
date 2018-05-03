package ci

import (
	"github.com/wodby/wodby-cli/cmd/wodby/ci/build"
	"github.com/wodby/wodby-cli/cmd/wodby/ci/deploy"
	"github.com/wodby/wodby-cli/cmd/wodby/ci/init"
	"github.com/wodby/wodby-cli/cmd/wodby/ci/release"
	"github.com/wodby/wodby-cli/cmd/wodby/ci/run"
	"github.com/spf13/cobra"
)

// Cmd represents the ci command.
var Cmd = &cobra.Command{
	Use:   "ci [command]",
	Short: "ci commands",
}

func init() {
	Cmd.AddCommand(init.Cmd)
	Cmd.AddCommand(build.Cmd)
	Cmd.AddCommand(release.Cmd)
	Cmd.AddCommand(deploy.Cmd)
	Cmd.AddCommand(run.Cmd)
}
