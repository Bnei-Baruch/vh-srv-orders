package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

// The charge path sees real traffic once a month, in one burst at 02:00 on the
// 20th, so a credential that does not land is invisible until it has failed a
// month of renewals.
//
// It charges nothing: the card token it sends is hard-coded and invalid, so
// the gateway declines it. Only a 200 decline is a pass. Everything else fails
// the command: a rejected or missing credential, a malformed request, a wrong
// URL, a 5xx, a timeout, and above all an accepted card.
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
	chargeCheckCmd.Flags().String("url", "",
		"override the charge URL, e.g. https://checkout.kbb1.com/vh/token/charge — that route "+
			"requires a caller, so a 401 there is a definitive answer where the shared route's "+
			"observe mode accepts anonymous callers too")
}

func chargeCheckFn(cmd *cobra.Command, args []string) {
	terminalName, _ := cmd.Flags().GetString("terminal")
	reference, _ := cmd.Flags().GetString("reference")
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
		// Invalid on purpose: the gateway declines it, so nothing is charged.
		// Not a flag, so a real token can't be pasted in.
		Token: chargeCheckCardToken,
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
	status, err := chargeCheckVerdict(result, err)
	if err != nil {
		utils.LogFatal("FAILED", slog.String("terminal", terminal.Name),
			slog.String("url", terminal.ChargeURL), slog.Any("err", err))
	}
	log.Info("PASSED: the request reached the gateway and the card was refused",
		slog.String("terminal", terminal.Name),
		slog.String("url", terminal.ChargeURL),
		slog.String("gateway_status", status),
		slog.Any("gateway_error", result["error"]))
}

const chargeCheckCardToken = "0000000000000000"

// errCardAccepted means the gateway charged the card. The token is invalid, so
// this should never happen; if it does, money moved and someone must look.
var errCardAccepted = errors.New("the gateway ACCEPTED the card: money moved")

// chargeCheckVerdict passes only a 200 whose body is not a success, i.e. the
// request got past the credential and validation and the card was refused.
// external_payments answers 200 for a declined card and puts the reason in the
// body; the renewal path reads this same field, so this does too.
func chargeCheckVerdict(result map[string]any, err error) (string, error) {
	if err != nil {
		// The error already names ErrUnauthorized, ErrNoCredential, [400], [404], …
		return "", fmt.Errorf("the charge did not reach the gateway and get declined: %w", err)
	}
	status, _ := result["status"].(string)
	if status == common.PaymentStatusSuccess {
		return status, errCardAccepted
	}
	return status, nil
}
