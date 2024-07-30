package cmd

import (
	"github.com/spf13/cobra"
	"gitlab.bbdev.team/vh/pay/orders/charge"
)

var chargeCmd = &cobra.Command{
	Use:   "charge",
	Short: "Run the monthly charge for recurring orders",
	Run: func(cmd *cobra.Command, args []string) {
		charge.MonthlyCharge()
	},
}

func init() {
	rootCmd.AddCommand(chargeCmd)
}
