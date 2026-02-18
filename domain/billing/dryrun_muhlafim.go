package billing

import (
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// Dry-run muhlafim simulation (deterministic from orderID):
// - 0.5% of orders are "matched" (have a muhlafim entry)
// - Of matched: 80% have a new card number (order stays flagged for renewal)
// - Of matched: 20% get a muh_ flag (order removed from renewal pool)
//
// The match hash h = orderID * 2654435761 % 1000 yields values 0..4 for matches.
// h < 4 (values 0,1,2,3 = 80%) -> new card; h == 4 (20%) -> muh_ flag.
const (
	dryRunMuhMatchThreshold = 5 // out of 1000 -> 0.5%
	dryRunMuhNewCardBucket  = 4 // h values 0..3 = new card, h == 4 = flag
)

var dryRunMuhActions = []string{
	pelecard.MUH_HIYUV_NIKLAT,
	pelecard.MUH_NIDHA,
	pelecard.MUH_BITUL,
	pelecard.MUH_LOTAKIN,
	"unknown", // triggers OrderFlagMuhAher via the default case
}

// generateDryRunMuhlafim creates simulated muhlafim data for dry-run mode.
// No Pelecard API call is made. Uses Knuth multiplicative hashing so results
// are deterministic and reproducible per orderID. Thread-safe (pure function).
func generateDryRunMuhlafim(tokenMap map[int]string) map[string]pelecard.MuhlafimEntry {
	result := make(map[string]pelecard.MuhlafimEntry)

	for orderID, token := range tokenMap {
		if token == "" {
			continue
		}

		// 0.5% of orders are "matched": h in {0,1,2,3,4}
		h := uint64(orderID) * 2654435761 % 1000
		if h >= dryRunMuhMatchThreshold {
			continue
		}

		// 80% new card (h < 4), 20% muh_ flag (h == 4)
		if h < dryRunMuhNewCardBucket {
			result[token] = pelecard.MuhlafimEntry{
				Token:             token,
				ActionDescription: pelecard.MUH_HIYUV_NIKLAT,
				NewCardNumber:     "0000000000001234",
				NewExpirationDate: "12/30",
			}
		} else {
			actionIdx := uint64(orderID) * 2246822519 % uint64(len(dryRunMuhActions))
			result[token] = pelecard.MuhlafimEntry{
				Token:             token,
				ActionDescription: dryRunMuhActions[actionIdx],
			}
		}
	}

	return result
}

// dryRunMuhFlag returns the expected order flag for a given orderID when it falls
// into the 20% "flag" bucket. Useful for testing.
func dryRunMuhFlag(orderID int) string {
	actionIdx := uint64(orderID) * 2246822519 % uint64(len(dryRunMuhActions))
	action := dryRunMuhActions[actionIdx]
	switch action {
	case pelecard.MUH_HIYUV_NIKLAT:
		return common.OrderFlagMuhHiyuvNiklat
	case pelecard.MUH_NIDHA:
		return common.OrderFlagMuhNidha
	case pelecard.MUH_BITUL:
		return common.OrderFlagMuhBitul
	case pelecard.MUH_LOTAKIN:
		return common.OrderFlagMuhLotakin
	default:
		return common.OrderFlagMuhAher
	}
}
