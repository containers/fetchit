package engine

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start harpooon engine",
	Long:  `Start harpoon engine`,
	Run: func(cmd *cobra.Command, args []string) {
		harpoonConfig.InitConfig(true)
		harpoonConfig.GetTargets()
		harpoonConfig.RunTargets()
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
