package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// ManualDiscountProvider fetches the active manual discount for a user.
// Implemented by *OrdersDB; injected into the pricing resolver.
type ManualDiscountProvider interface {
	GetActiveManualDiscount(ctx context.Context, keycloakID string) (*ManualDiscount, error)
}

// UpsertManualDiscount cancels any other active discounts for the user, then inserts or updates
// the record. If req.ID is set the existing row is updated; otherwise a new row is inserted.
// Both operations run in a transaction.
func (o *OrdersDB) UpsertManualDiscount(ctx context.Context, req ManualDiscountReq) (*ManualDiscount, error) {
	tx, err := o.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("o.Begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Cancel other active discounts for this user (exclude the row being updated, if any).
	_, err = tx.Exec(ctx,
		`UPDATE manual_discount
		 SET end_date = NOW() - INTERVAL '1 day', updated_at = NOW()
		 WHERE keycloak_id = $1 AND end_date > NOW() AND start_date <= NOW()
		   AND ($2::int IS NULL OR id != $2)`,
		req.KeycloakID, req.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("tx.Exec: %w", err)
	}

	var md ManualDiscount
	if req.ID != nil {
		err = tx.QueryRow(ctx,
			`UPDATE manual_discount
			 SET type = $2, properties = $3, start_date = $4, end_date = $5, note = $6, updated_at = NOW()
			 WHERE id = $1 AND keycloak_id = $7
			 RETURNING id, keycloak_id, start_date, end_date, updated_at, type, properties, note`,
			*req.ID, req.Type, req.Properties.JSON, req.StartDate, req.EndDate, req.Note, req.KeycloakID,
		).Scan(&md.ID, &md.KeycloakID, &md.StartDate, &md.EndDate, &md.UpdatedAt, &md.Type, &md.Properties, &md.Note)
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO manual_discount (keycloak_id, start_date, end_date, type, properties, note)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 RETURNING id, keycloak_id, start_date, end_date, updated_at, type, properties, note`,
			req.KeycloakID, req.StartDate, req.EndDate, req.Type, req.Properties.JSON, req.Note,
		).Scan(&md.ID, &md.KeycloakID, &md.StartDate, &md.EndDate, &md.UpdatedAt, &md.Type, &md.Properties, &md.Note)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, common.ErrNoRowsAffected
		}
		return nil, fmt.Errorf("tx.QueryRow.Scan: %w", err)
	}

	return &md, tx.Commit(ctx)
}


// CancelManualDiscount sets end_date to yesterday for the user's active discount.
// Returns ErrNoRowsAffected if there is no active discount.
func (o *OrdersDB) CancelManualDiscount(ctx context.Context, keycloakID string) error {
	res, err := o.Exec(ctx,
		`UPDATE manual_discount
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

// GetActiveManualDiscount returns the active discount for the user, or nil if none exists.
func (o *OrdersDB) GetActiveManualDiscount(ctx context.Context, keycloakID string) (*ManualDiscount, error) {
	var md ManualDiscount
	err := o.QueryRow(ctx,
		`SELECT id, keycloak_id, start_date, end_date, updated_at, type, properties, note
		 FROM manual_discount
		 WHERE keycloak_id = $1 AND end_date > NOW() AND start_date <= NOW()
		 ORDER BY id DESC
		 LIMIT 1`,
		keycloakID,
	).Scan(&md.ID, &md.KeycloakID, &md.StartDate, &md.EndDate, &md.UpdatedAt, &md.Type, &md.Properties, &md.Note)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}
	return &md, nil
}

// GetAllManualDiscounts returns all discount records, optionally filtered by exact keycloak_id, newest first.
func (o *OrdersDB) GetAllManualDiscounts(ctx context.Context, search string) ([]*ManualDiscount, error) {
	query := `SELECT id, keycloak_id, start_date, end_date, updated_at, type, properties, note
	          FROM manual_discount`
	var args []interface{}
	if search != "" {
		query += ` WHERE keycloak_id = $1`
		args = append(args, search)
	}
	query += ` ORDER BY id DESC`

	rows, err := o.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var result []*ManualDiscount
	for rows.Next() {
		var md ManualDiscount
		if err := rows.Scan(&md.ID, &md.KeycloakID, &md.StartDate, &md.EndDate, &md.UpdatedAt, &md.Type, &md.Properties, &md.Note); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		result = append(result, &md)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}
	return result, nil
}
