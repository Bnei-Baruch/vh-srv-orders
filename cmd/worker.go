package cmd

import (
	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Workers commands, please use one of the sub-commands",
}

var specialCmd = &cobra.Command{
	Use:   "updateSpecial",
	Short: "Update the membership for accounts of the current date",
	Run: func(cmd *cobra.Command, args []string) {
		Do(NewWorker())
	},
}

func init() {
	rootCmd.AddCommand(workerCmd)
	workerCmd.AddCommand(specialCmd)
}
