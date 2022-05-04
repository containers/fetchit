package engine

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start harpooon engine",
	Long:  `Start fetchit engine`,
	Run: func(cmd *cobra.Command, args []string) {
		fetchitConfig.InitConfig(true)
		fetchitConfig.GetTargets()
		fetchitConfig.RunTargets()
	},
}

func init() {
	fetchitCmd.AddCommand(startCmd)
	fetchitConfig = NewFetchitConfig()
	cmdGroup := []*cobra.Command{
		fetchitCmd,
		startCmd,
	}
	for _, cmd := range cmdGroup {
		fetchitConfig.bindFlags(cmd)
		if cmd.Flags().Changed("config") {
			configFile, err := cmd.Flags().GetString("config")
			if err != nil {
				cobra.CheckErr(err)
			}
			fetchitConfig.configFile = configFile
		}
	}
}
