package engine

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start harpooon engine",
	Long:  `Start harpoon engine`,
	Run: func(cmd *cobra.Command, args []string) {
		harpoonConfig.initConfig(cmd)
		if len(harpoonConfig.Targets) == 0 {
			panic("no harpoon target repositories collected")
		}
		harpoonConfig.runTargets()
	},
}

func init() {
	harpoonCmd.AddCommand(startCmd)
	harpoonConfig = NewHarpoonConfig()
	cmdGroup := []*cobra.Command{
		harpoonCmd,
		startCmd,
	}
	for _, cmd := range cmdGroup {
		harpoonConfig.bindFlags(cmd)
	}
}
