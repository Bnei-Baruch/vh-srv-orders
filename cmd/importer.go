package cmd

import (
	"github.com/spf13/cobra"
	"gitlab.bbdev.team/vh/pay/orders/importers"
)

var importerCmd = &cobra.Command{
	Use:   "import",
	Short: "Import commands, please use one of the sub-commands",
}

var robokasaCmd = &cobra.Command{
	Use:   "robokasa",
	Short: "Import Russian offline payments for membership",
	Run: func(cmd *cobra.Command, args []string) {
		importers.ImportRobokasa()
	},
}

var genericOfflineCmd = &cobra.Command{
	Use:   "offline",
	Short: "Import offline payments for membership",
	Run: func(cmd *cobra.Command, args []string) {
		importers.ImportGeneric()
	},
}

var importSpecialsCmd = &cobra.Command{
	Use:   "specials",
	Short: "Import specials for membership",
	Run: func(cmd *cobra.Command, args []string) {
		importers.ImportSpecials()
	},
}

func init() {
	importerCmd.AddCommand(robokasaCmd)
	importerCmd.AddCommand(genericOfflineCmd)
	importerCmd.AddCommand(importSpecialsCmd)
	rootCmd.AddCommand(importerCmd)
}
