package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

var muhlafimCheckCmd = &cobra.Command{
	Use:   "muhlafim-check",
	Short: "Compare muhlafim from Pelecard directly against external_payments",
	Long: "Fetches the same windows from both sources and reports whether they agree,\n" +
		"entry by entry and field by field. Touches no database and writes nothing —\n" +
		"it exists to prove external_payments queries the same terminal, over enough\n" +
		"windows to trust, before the direct Pelecard call is removed.",
	Run: muhlafimCheckFn,
}

func init() {
	pelecardCmd.AddCommand(muhlafimCheckCmd)

	muhlafimCheckCmd.Flags().StringArray("range", nil,
		"Window as \"DD/MM/YYYY HH:MM..DD/MM/YYYY HH:MM\". Repeat for several windows.")
	muhlafimCheckCmd.MarkFlagRequired("range")
	muhlafimCheckCmd.Flags().Bool("show-tokens", false,
		"Print masked tokens for entries that disagree")
}

type muhlafimWindow struct {
	start string
	end   string
}

func muhlafimCheckFn(cmd *cobra.Command, args []string) {
	windows := parseRanges(cmd)
	showTokens, _ := cmd.Flags().GetBool("show-tokens")

	ctx := context.Background()
	client := pelecard.NewClient()

	var disagreed int
	for _, window := range windows {
		if !compareWindow(ctx, client, window, showTokens) {
			disagreed++
		}
	}

	fmt.Printf("\n%d window(s) compared, %d disagreed\n", len(windows), disagreed)
	if disagreed > 0 {
		fmt.Println("sources DISAGREE — do not remove the direct call")
		return
	}
	fmt.Println("sources agree on every window")
}

// compareWindow reports whether the two sources agree, and returns false if they
// do not. Every field of every entry is compared, not just the totals: a source
// pointed at the wrong terminal could return the right number of rows.
func compareWindow(ctx context.Context, client *pelecard.Client, window muhlafimWindow, showTokens bool) bool {
	fmt.Printf("\n=== %s .. %s ===\n", window.start, window.end)

	direct, directErr := client.FetchMuhlafim(ctx, window.start, window.end)
	if directErr != nil {
		utils.LogFor(ctx).Error("pelecard direct fetch failed", slog.Any("error", directErr))
	}

	external, externalErr := client.FetchMuhlafimExternal(ctx, window.start, window.end)
	if externalErr != nil {
		utils.LogFor(ctx).Error("external_payments fetch failed", slog.Any("error", externalErr))
	}

	if directErr != nil || externalErr != nil {
		fmt.Println("cannot compare: at least one source failed")
		return false
	}

	fmt.Printf("pelecard direct     %d entries\n", len(direct))
	fmt.Printf("external_payments   %d entries\n", len(external))

	onlyDirect := missingFrom(direct, external)
	onlyExternal := missingFrom(external, direct)
	differing := differingEntries(direct, external)

	fmt.Printf("only in pelecard    %d\n", len(onlyDirect))
	fmt.Printf("only in external    %d\n", len(onlyExternal))
	fmt.Printf("differing content   %d\n", len(differing))

	matched := len(direct) - len(onlyDirect) - len(differing)
	fmt.Printf("identical entries   %d (all of Token, ActionDescription, NewCardNumber, NewExpirationDate)\n", matched)

	if showTokens {
		printSample("only in pelecard", onlyDirect)
		printSample("only in external", onlyExternal)
	}

	for _, token := range differing {
		fmt.Printf("  %s\n", maskToken(token))
		for _, line := range diffFields(direct[token], external[token]) {
			fmt.Printf("    %s\n", line)
		}
	}

	agreed := len(onlyDirect) == 0 && len(onlyExternal) == 0 && len(differing) == 0
	if agreed {
		fmt.Println("agree")
	} else {
		fmt.Println("DISAGREE")
	}
	return agreed
}

func parseRanges(cmd *cobra.Command) []muhlafimWindow {
	raw, err := cmd.Flags().GetStringArray("range")
	if err != nil {
		utils.LogFatal("Failed to read range flag", slog.Any("error", err))
	}

	windows := make([]muhlafimWindow, 0, len(raw))
	for _, r := range raw {
		start, end, found := strings.Cut(r, "..")
		if !found {
			utils.LogFatal("range must be \"START..END\"", slog.String("range", r))
		}
		start, end = strings.TrimSpace(start), strings.TrimSpace(end)

		// Parsed only to reject a malformed window before it reaches Pelecard;
		// both sources take the string form.
		if _, err := parsePelecardDate(start); err != nil {
			utils.LogFatal("bad start date", slog.String("date", start), slog.Any("error", err))
		}
		if _, err := parsePelecardDate(end); err != nil {
			utils.LogFatal("bad end date", slog.String("date", end), slog.Any("error", err))
		}

		windows = append(windows, muhlafimWindow{start: start, end: end})
	}
	return windows
}

func missingFrom(from, other map[string]pelecard.MuhlafimEntry) []string {
	var tokens []string
	for token := range from {
		if _, ok := other[token]; !ok {
			tokens = append(tokens, token)
		}
	}
	sort.Strings(tokens)
	return tokens
}

func differingEntries(direct, external map[string]pelecard.MuhlafimEntry) []string {
	var tokens []string
	for token, d := range direct {
		e, ok := external[token]
		if ok && d != e {
			tokens = append(tokens, token)
		}
	}
	sort.Strings(tokens)
	return tokens
}

func diffFields(direct, external pelecard.MuhlafimEntry) []string {
	var lines []string
	if direct.ActionDescription != external.ActionDescription {
		lines = append(lines, fmt.Sprintf("ActionDescription: pelecard=%q external=%q",
			direct.ActionDescription, external.ActionDescription))
	}
	if direct.NewCardNumber != external.NewCardNumber {
		lines = append(lines, fmt.Sprintf("NewCardNumber: pelecard=%s external=%s",
			maskCard(direct.NewCardNumber), maskCard(external.NewCardNumber)))
	}
	if direct.NewExpirationDate != external.NewExpirationDate {
		lines = append(lines, fmt.Sprintf("NewExpirationDate: pelecard=%q external=%q",
			direct.NewExpirationDate, external.NewExpirationDate))
	}
	return lines
}

func printSample(label string, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	fmt.Printf("%s:\n", label)
	for i, token := range tokens {
		if i == 10 {
			fmt.Printf("  ... and %d more\n", len(tokens)-10)
			break
		}
		fmt.Printf("  %s\n", maskToken(token))
	}
}

// Tokens identify stored cards, so print only enough to tell them apart.
func maskToken(token string) string {
	if len(token) <= 6 {
		return "***"
	}
	return "***" + token[len(token)-6:]
}

func maskCard(card string) string {
	if len(card) <= 4 {
		return card
	}
	return "***" + card[len(card)-4:]
}
