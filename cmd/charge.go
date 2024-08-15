package cmd

import (
	"log"
	"log/slog"

	"github.com/spf13/cobra"
	"gitlab.bbdev.team/vh/pay/orders/charge"
)

var chargeCmd = &cobra.Command{
	Use:   "charge",
	Short: "Charge commands, please use one of the sub-commands",
}

var monthlyChargeCmd = &cobra.Command{
	Use:   "monthly",
	Short: "Run the monthly charge for recurring orders",
	Run: func(cmd *cobra.Command, args []string) {
		charge.MonthlyCharge()
	},
}

var muhlafimCmd = &cobra.Command{
	Use:   "muhlafim",
	Short: "Load pelecard muhlafim from file",
	Run: func(cmd *cobra.Command, args []string) {
		res, err := charge.LoadMuhlafimFromFile("/Users/edoshor/projects/vh/data/vh-report-cli/muh.json")
		if err != nil {
			log.Fatalf("error loading muhlafim from file: %+v", err)
		}
		slog.Info("loaded muhlafim", slog.Int("count", len(res)))
	},
}

func init() {
	chargeCmd.AddCommand(monthlyChargeCmd)
	chargeCmd.AddCommand(muhlafimCmd)
	rootCmd.AddCommand(chargeCmd)
}
