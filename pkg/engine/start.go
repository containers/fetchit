package engine

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start harpooon engine",
	Long:  `Start fetchit engine`,
	Run: func(cmd *cobra.Command, args []string) {
		fetchit = fetchitConfig.InitConfig(true)
		fetchit.RunTargets()
	},
}

func init() {
	fetchitConfig = newFetchitConfig()
	fetchitCmd.AddCommand(startCmd)
}
