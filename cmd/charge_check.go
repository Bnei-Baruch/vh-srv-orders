package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// The charge path sees real traffic once a month, in one burst at 02:00 on the
// 20th, so a credential that does not land is invisible until it has failed a
// month of renewals.
//
// It charges nothing: the card token it sends is invalid, so the gateway
// declines it. A 401 means the credential was rejected; anything else means it
// was accepted and the request reached the gateway. A decline is a pass.
//
// The default target /token/charge is in observe mode, so a pass there only
// proves the credential was not rejected — checkout's log settles the rest,
// showing `requested_by=keycloak:vh`. Point --url at /vh/token/charge, which
// requires a caller, for an answer that stands on its own.
var chargeCheckCmd = &cobra.Command{
	Use:   "charge-check",
	Short: "Verify that charge calls authenticate to external_payments",
	Long: "Sends one charge request with an intentionally invalid card token and reports whether " +
		"external_payments accepted the credential. Moves no money: the card is refused by the " +
		"gateway, so only the authentication in front of it is exercised.",
	Run: chargeCheckFn,
}

func init() {
	pelecardCmd.AddCommand(chargeCheckCmd)

	chargeCheckCmd.Flags().String("terminal", "token", "which terminal to check: token or emv")
	chargeCheckCmd.Flags().String("reference", "", "reference to send (default: m-authcheck-<pid>)")
	chargeCheckCmd.Flags().String("card-token", "0000000000000000",
		"card token to send — invalid on purpose, so nothing can be charged")
	chargeCheckCmd.Flags().String("url", "",
		"override the charge URL, e.g. https://checkout.kbb1.com/vh/token/charge — that route "+
			"requires a caller, so a 401 there is a definitive answer where the shared route's "+
			"observe mode accepts anonymous callers too")
}

func chargeCheckFn(cmd *cobra.Command, args []string) {
	terminalName, _ := cmd.Flags().GetString("terminal")
	reference, _ := cmd.Flags().GetString("reference")
	cardToken, _ := cmd.Flags().GetString("card-token")
	urlOverride, _ := cmd.Flags().GetString("url")

	if common.Config.KeycloakClientID == "" || common.Config.KeycloakClientSecret == "" {
		utils.LogFatal("KEYCLOAK_CLIENT_ID and KEYCLOAK_CLIENT_SECRET are required")
	}

	var terminal pelecard.Terminal
	switch terminalName {
	case "token":
		terminal = pelecard.TokenTerminal
	case "emv":
		terminal = pelecard.EMVTerminal
	default:
		utils.LogFatal("unknown terminal", slog.String("terminal", terminalName))
	}

	if urlOverride != "" {
		terminal.ChargeURL = urlOverride
	}

	if reference == "" {
		// Unique per run and obviously not a payment, so a row left by a declined
		// attempt is not mistaken for a member's charge.
		reference = fmt.Sprintf("m-authcheck-%d", os.Getpid())
	}

	ctx := context.Background()
	log := utils.LogFor(ctx)
	client := pelecard.NewClient()

	log.Info("checking charge authentication",
		slog.String("url", terminal.ChargeURL),
		slog.String("terminal", terminal.Name),
		slog.String("reference", reference),
		slog.String("keycloak_client", common.Config.KeycloakClientID))

	// Deliberately unchargeable, and otherwise identical to a renewal, so it is
	// rejected on the card rather than on validation.
	request := &pelecard.ChargeRequest{
		UserKey: reference,
		Token:   cardToken,
		// Required by validation, which runs before the card is touched. Never
		// navigated to.
		GoodURL:      "https://example.invalid/good",
		ErrorURL:     "https://example.invalid/error",
		CancelURL:    "https://example.invalid/cancel",
		Price:        1,
		Currency:     "NIS",
		Name:         "Charge authentication check",
		Email:        "noreply@kab.co.il",
		Phone:        "+NA",
		Street:       "NA",
		City:         "NA",
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          "40037",
		VAT:          "f",
		Installments: 1,
		Language:     "HE",
		Reference:    reference,
		Organization: "ben2",
	}

	result, err := client.ChargeByToken(ctx, request, terminal)

	switch {
	case err == nil:
		// external_payments answers 200 for a declined card and puts the reason in
		// the body. The renewal path reads this same field rather than the HTTP
		// status, so this does too.
		status, _ := result["status"].(string)
		if status == "success" {
			log.Warn("PASSED, but the gateway accepted an invalid card token — worth investigating",
				slog.String("terminal", terminal.Name),
				slog.String("card_token", cardToken))
			return
		}

		log.Info("PASSED: the request reached the gateway and the card was refused",
			slog.String("terminal", terminal.Name),
			slog.String("url", terminal.ChargeURL),
			slog.String("gateway_status", status),
			slog.Any("gateway_error", result["error"]))

	case errors.Is(err, pelecard.ErrUnauthorized):
		log.Error("FAILED: external_payments rejected the credential",
			slog.String("terminal", terminal.Name),
			slog.Any("err", err))
		utils.LogFatal("charge calls would fail for every renewal in a run")

	case strings.Contains(err.Error(), "[400]"):
		// Rejected as malformed, before the card and before the credential
		// matters, so it proves nothing either way.
		log.Error("INCONCLUSIVE: external_payments rejected the request as malformed",
			slog.String("terminal", terminal.Name),
			slog.Any("err", err))
		utils.LogFatal("the request never reached the gateway, so nothing was proved")

	default:
		// Past validation, so the credential was not what stopped it.
		log.Info("PASSED: the request was not rejected on its credential",
			slog.String("terminal", terminal.Name),
			slog.String("url", terminal.ChargeURL),
			slog.String("gateway_error", err.Error()))
	}
}
