package version

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/version"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Shows Wodby CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.VERSION)
	},
}
