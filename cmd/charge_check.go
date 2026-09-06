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

// The charge path sees real traffic once a month, in a single burst of ~3,500
// calls at 02:00 on the 20th. A credential that does not land is therefore
// invisible until it has already failed a month of renewals — the same shape of
// problem the muhlafim comparison command was built for, and the reason this
// exists before the charge routes start requiring a caller.
//
// It charges nothing. The token it sends is deliberately invalid, so Pelecard
// declines it; what is being tested is everything in front of the card:
//
//	401  -> the credential was rejected; every charge in a run would fail
//	other -> the credential was accepted and the request reached the gateway
//
// A decline is therefore a pass. That inversion is the whole trick, and it is
// why the command reports what it is asserting rather than just an exit code.
//
// What a pass does and does not mean, on the route this currently targets: the
// shared /token/charge is in observe mode, so it accepts anonymous callers too,
// and a pass there proves only that the credential was not *rejected*.
// checkout's own log settles the rest, showing `requested_by=keycloak:vh`
// against `anonymous`. Point --url at /vh/token/charge for an answer that
// stands on its own: that route requires a caller, so a 401 is definitive and a
// pass means the bearer was accepted. Once the terminals move there, the
// default target becomes definitive too.
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
		// Unique per run and obviously not a payment, so a row left behind by a
		// declined attempt cannot be mistaken for a member's charge.
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

	// Deliberately unchargeable: an invalid card token with the smallest amount
	// the gateway will look at. Everything else mirrors what a renewal sends, so
	// the request is rejected on the card rather than on validation.
	request := &pelecard.ChargeRequest{
		UserKey: reference,
		Token:   cardToken,
		// Required by external_payments' validation, and it runs before the card
		// is touched — leave them out and the request is rejected as malformed,
		// which proves nothing about the credential. Never navigated to: the
		// gateway refuses the card before any redirect is issued.
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
		// external_payments answers 200 for a declined card — it cannot yet tell a
		// decline from a gateway outage, so it reports both as success-shaped and
		// puts the reason in the body. VH's renewal path reads this same field
		// rather than the HTTP status, so this command has to as well.
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
		// Rejected as malformed, before the card and before anything that depends
		// on the credential. Reporting this as a pass would be the command lying
		// about what it exercised.
		log.Error("INCONCLUSIVE: external_payments rejected the request as malformed",
			slog.String("terminal", terminal.Name),
			slog.Any("err", err))
		utils.LogFatal("the request never reached the gateway, so nothing was proved")

	default:
		// Some other HTTP failure — the request still got past validation, so the
		// credential was not what stopped it.
		log.Info("PASSED: the request was not rejected on its credential",
			slog.String("terminal", terminal.Name),
			slog.String("url", terminal.ChargeURL),
			slog.String("gateway_error", err.Error()))
	}
}
