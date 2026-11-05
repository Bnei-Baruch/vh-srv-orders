package cmd

import (
	"errors"
	"fmt"
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

func TestChargeCheckVerdict(t *testing.T) {
	cases := []struct {
		name   string
		result map[string]any
		err    error
		pass   bool
	}{
		{"credential rejected", nil, fmt.Errorf("%w: [401]", pelecard.ErrUnauthorized), false},
		{"no credential", nil, fmt.Errorf("%w: login failed", pelecard.ErrNoCredential), false},
		{"malformed request", nil, errors.New("[400] bad request"), false},
		{"forbidden", nil, errors.New("[403] forbidden"), false},
		{"wrong url", nil, errors.New("[404] not found"), false},
		{"server error", nil, errors.New("[502] bad gateway"), false},
		{"connection refused", nil, errors.New("dial tcp: connection refused"), false},
		{"declined", map[string]any{"status": "error", "error": "Charge error "}, nil, true},
		{"accepted", map[string]any{"status": "success"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := chargeCheckVerdict(c.result, c.err)
			if (err == nil) != c.pass {
				t.Fatalf("pass = %v, want %v (err: %v)", err == nil, c.pass, err)
			}
			if c.err != nil && !errors.Is(err, c.err) {
				t.Fatalf("verdict lost the cause: %v", err)
			}
		})
	}
	if _, err := chargeCheckVerdict(map[string]any{"status": "success"}, nil); !errors.Is(err, errCardAccepted) {
		t.Fatalf("an accepted card must report errCardAccepted, got %v", err)
	}
}
