package cmd

import (
	"github.com/spf13/cobra"
	"gitlab.bbdev.team/vh/pay/orders/importers"
)

var robokasaCmd = &cobra.Command{
	Use:   "robokasa",
	Short: "Import Russian offline payments for membership",
	Run: func(cmd *cobra.Command, args []string) {
		importers.ImportRobokasa()
	},
}

func init() {
	rootCmd.AddCommand(robokasaCmd)
}
