package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

const (
	operationTypeEmailUpdate = "email_update"
	operationStatusSuccess   = "success"
	operationStatusReverted  = "reverted"
)

// emailInput is what operation_trace.input stores: the change as requested.
// RevertOperation re-applies it with old and new swapped, so no SQL is ever
// stored or read back.
type emailInput struct {
	NewEmail      *string `json:"new_email"`
	NewKeycloakID *string `json:"new_keycloak_id"`
	OldKeycloakID *string `json:"old_keycloak_id"`
	OldEmail      *string `json:"old_email"`
}

// identityChange is one direction of an email_update: from → to.
// With fromKc nil only the email changes, on the account whose key is toKc.
type identityChange struct {
	fromEmail, toEmail string
	fromKc             *string
	toKc               string
}

func (in emailInput) forward() (identityChange, error) {
	if in.NewEmail == nil || in.OldEmail == nil || in.NewKeycloakID == nil {
		return identityChange{}, fmt.Errorf("%w: new_email, old_email and new_keycloak_id are required", common.ErrInvalidValues)
	}
	return identityChange{fromEmail: *in.OldEmail, toEmail: *in.NewEmail, fromKc: in.OldKeycloakID, toKc: *in.NewKeycloakID}, nil
}

func (c identityChange) reverse() identityChange {
	if c.fromKc == nil {
		return identityChange{fromEmail: c.toEmail, toEmail: c.fromEmail, toKc: c.toKc}
	}
	fromKc := c.toKc
	return identityChange{fromEmail: c.toEmail, toEmail: c.fromEmail, fromKc: &fromKc, toKc: *c.fromKc}
}

// apply runs the change in tx and returns the rows each step touched.
//
// Only tables that hold the user's email or keycloak id change. payments
// "Ordkey" and payments_pelecard ord_key hold order keys ("ord-…"), never a
// keycloak id, so they are not touched.
func (o *OrdersDB) applyIdentityChange(ctx context.Context, tx pgx.Tx, c identityChange) (map[string]int64, error) {
	type step struct {
		name string
		sql  string
		args []any
	}
	var steps []step

	if c.fromKc == nil {
		steps = []step{
			{"accounts", `UPDATE accounts SET "Email" = $1 WHERE "Email" = $2 AND "UserKey" = $3`, []any{c.toEmail, c.fromEmail, c.toKc}},
			{"specials", `UPDATE specials SET email = $1 WHERE email = $2`, []any{c.toEmail, c.fromEmail}},
		}
	} else {
		// Moving an account onto a key that already has one would give that key
		// two accounts, which accounts_userkey_uniq refuses. Say so before
		// changing anything; folding two people together is MergeAccountsOrders.
		if *c.fromKc != c.toKc {
			if _, err := o.GetAccountIDByKeycloakID(ctx, c.toKc); err == nil {
				return nil, fmt.Errorf("new keycloak id: %w; merge the accounts instead", common.ErrAccountKeyTaken)
			} else if !errors.Is(err, common.ErrNoRowsAffected) {
				return nil, fmt.Errorf("o.GetAccountIDByKeycloakID: %w", err)
			}
		}
		steps = []step{
			{"accounts", `UPDATE accounts SET "Email" = $1, "UserKey" = $2 WHERE "Email" = $3 AND "UserKey" = $4`, []any{c.toEmail, c.toKc, c.fromEmail, *c.fromKc}},
			{"orders", `UPDATE orders SET userkey = $1 WHERE userkey = $2`, []any{c.toKc, *c.fromKc}},
			{"specials.email", `UPDATE specials SET email = $1 WHERE email = $2`, []any{c.toEmail, c.fromEmail}},
			{"specials.keycloak_id", `UPDATE specials SET keycloak_id = $1 WHERE keycloak_id = $2`, []any{c.toKc, *c.fromKc}},
		}
	}

	rows := make(map[string]int64, len(steps))
	for _, s := range steps {
		res, err := tx.Exec(ctx, s.sql, s.args...)
		if err != nil {
			return nil, fmt.Errorf("update %s: %w", s.name, err)
		}
		rows[s.name] = res.RowsAffected()
	}
	return rows, nil
}

func (o *OrdersDB) PerformOperation(ctx context.Context, req OperationReq) (int, error) {
	input := emailInput{NewEmail: req.NewEmail, NewKeycloakID: req.NewKeycloakID, OldKeycloakID: req.OldKeycloakID, OldEmail: req.OldEmail}
	change, err := input.forward()
	if err != nil {
		return 0, err
	}

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("o.pool.Begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := o.applyIdentityChange(ctx, tx, change)
	if err != nil {
		return 0, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return 0, fmt.Errorf("marshal input: %w", err)
	}
	outputJSON, err := json.Marshal(rows)
	if err != nil {
		return 0, fmt.Errorf("marshal output: %w", err)
	}

	var id int
	if err := tx.QueryRow(ctx,
		`INSERT INTO operation_trace (input, output, status, type) VALUES ($1, $2, $3, $4) RETURNING id`,
		string(inputJSON), string(outputJSON), operationStatusSuccess, operationTypeEmailUpdate).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert operation_trace: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("tx.Commit: %w", err)
	}
	return id, nil
}

// RevertOperation undoes the latest operation from oldEmail to newEmail by
// applying its input in reverse, and records the rows that touched.
func (o *OrdersDB) RevertOperation(ctx context.Context, newEmail string, oldEmail string) error {
	var (
		id     int
		status string
		raw    string
	)
	if err := o.pool.QueryRow(ctx,
		`SELECT id, status, input::text FROM operation_trace
		 WHERE input->>'new_email' = $1 AND input->>'old_email' = $2
		 ORDER BY id DESC LIMIT 1`, newEmail, oldEmail).Scan(&id, &status, &raw); err != nil {
		return fmt.Errorf("select operation_trace: %w", err)
	}
	if status == operationStatusReverted {
		return fmt.Errorf("operation already reverted")
	}

	var input emailInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return fmt.Errorf("unmarshal operation_trace input: %w", err)
	}
	change, err := input.forward()
	if err != nil {
		return err
	}

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("o.pool.Begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := o.applyIdentityChange(ctx, tx, change.reverse())
	if err != nil {
		return err
	}
	revertJSON, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("marshal revert: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE operation_trace SET revert = $1, status = $2 WHERE id = $3`,
		string(revertJSON), operationStatusReverted, id); err != nil {
		return fmt.Errorf("update operation_trace: %w", err)
	}
	return tx.Commit(ctx)
}
