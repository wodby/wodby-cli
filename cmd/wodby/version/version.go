package version

import (
	"fmt"

	"github.com/wodby/wodby-cli/pkg/version"
	"github.com/spf13/cobra"
)

// Cmd represents the version command.
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Shows Wodby CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.RELEASE)
	},
}
