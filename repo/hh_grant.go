package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// HHGrantProvider fetches the active Help Haver grant for a user.
// Implemented by *OrdersDB; injected into the pricing resolver.
type HHGrantProvider interface {
	GetActiveHHGrant(ctx context.Context, keycloakID string) (*HHGrant, error)
}

const hhGrantColumns = `id, request_id, keycloak_id, type, discount_pct, start_date, end_date, updated_at, note`

// CancelHHGrant sets end_date to yesterday for the user's active grant.
// Returns ErrNoRowsAffected if there is no active grant.
func (o *OrdersDB) CancelHHGrant(ctx context.Context, keycloakID string) error {
	res, err := o.Exec(ctx,
		`UPDATE hh_grants
		 SET end_date = NOW() - INTERVAL '1 day', updated_at = NOW()
		 WHERE keycloak_id = $1 AND end_date > NOW() AND start_date <= NOW()`,
		keycloakID,
	)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}
	return nil
}

// GetActiveHHGrant returns the active grant for the user, or nil if none exists.
func (o *OrdersDB) GetActiveHHGrant(ctx context.Context, keycloakID string) (*HHGrant, error) {
	var g HHGrant
	err := o.QueryRow(ctx,
		`SELECT `+hhGrantColumns+`
		 FROM hh_grants
		 WHERE keycloak_id = $1 AND end_date > NOW() AND start_date <= NOW()
		 ORDER BY id DESC
		 LIMIT 1`,
		keycloakID,
	).Scan(&g.ID, &g.RequestID, &g.KeycloakID, &g.Type, &g.DiscountPct, &g.StartDate, &g.EndDate, &g.UpdatedAt, &g.Note)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return &g, nil
}
