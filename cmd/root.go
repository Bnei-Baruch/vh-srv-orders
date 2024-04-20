package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "orders",
	Short:   "Virtual home orders and payments management",
	Version: "x",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(common.LoadConfig)
	rootCmd.SetVersionTemplate(fmt.Sprintf("%s -- %s", common.ServiceName, common.GitSHA))

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
}
