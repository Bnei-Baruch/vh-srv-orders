package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// sqlQuery is one parameterized statement. operation_trace keeps them as a
// record of what ran; a revert never runs SQL read back from the table.
type sqlQuery struct {
	SQL  string   `json:"sql"`
	Args []string `json:"args"`
}

func (q sqlQuery) args() []any {
	args := make([]any, len(q.Args))
	for i, a := range q.Args {
		args[i] = a
	}
	return args
}

// renameStatements moves fromEmail to toEmail. With fromKc set, the account
// and every row keyed by it also move from fromKc to toKc. Without it, only
// the email of toKc's account changes. A revert is the same call with the
// sides swapped.
func renameStatements(fromEmail, toEmail string, fromKc *string, toKc string) []sqlQuery {
	var q []sqlQuery
	if fromKc == nil {
		q = append(q, sqlQuery{`UPDATE accounts SET "Email" = $1 WHERE "Email" = $2 AND "UserKey" = $3`,
			[]string{toEmail, fromEmail, toKc}})
	} else {
		q = append(q,
			sqlQuery{`UPDATE accounts SET "Email" = $1, "UserKey" = $2 WHERE "Email" = $3 AND "UserKey" = $4`,
				[]string{toEmail, toKc, fromEmail, *fromKc}},
			sqlQuery{`UPDATE orders SET userkey = $1 WHERE userkey = $2`, []string{toKc, *fromKc}},
			sqlQuery{`UPDATE payments_pelecard SET ord_key = $1 WHERE ord_key = $2`, []string{toKc, *fromKc}},
			sqlQuery{`UPDATE payments SET "Ordkey" = $1 WHERE "Ordkey" = $2`, []string{toKc, *fromKc}},
		)
	}
	return append(q,
		sqlQuery{`UPDATE specials SET email = $1 WHERE email = $2`, []string{toEmail, fromEmail}},
		sqlQuery{`UPDATE specials_sep2021 SET email = $1 WHERE email = $2`, []string{toEmail, fromEmail}},
	)
}

// revertStatements undoes renameStatements for the same input.
func revertStatements(in emailInput) []sqlQuery {
	if in.OldKeycloakID == nil {
		return renameStatements(*in.NewEmail, *in.OldEmail, nil, *in.NewKeycloakID)
	}
	return renameStatements(*in.NewEmail, *in.OldEmail, in.NewKeycloakID, *in.OldKeycloakID)
}

type QueryLog struct {
	Queries []sqlQuery `json:"queries"`
	Logs    []string   `json:"logs"`
}

type emailInput struct {
	NewEmail      *string `json:"new_email"`
	NewKeycloakID *string `json:"new_keycloak_id"`
	OldKeycloakID *string `json:"old_keycloak_id"`
	OldEmail      *string `json:"old_email"`
}

func (o *OrdersDB) PerformOperation(ctx context.Context, req OperationReq) (int, error) {

	input := emailInput{
		NewEmail:      req.NewEmail,
		NewKeycloakID: req.NewKeycloakID,
		OldKeycloakID: req.OldKeycloakID,
		OldEmail:      req.OldEmail,
	}
	var output QueryLog
	revert := QueryLog{Queries: revertStatements(input), Logs: []string{}}

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// run loop for all queries
	for _, query := range renameStatements(*input.OldEmail, *input.NewEmail, input.OldKeycloakID, *input.NewKeycloakID) {
		updatedRes, err := tx.Exec(ctx, query.SQL, query.args()...)

		if err != nil {
			// The new key already has an account: that is a merge, not a rename.
			if isKeyTaken(err) {
				return 0, fmt.Errorf("new keycloak id: %w; merge the accounts instead", common.ErrAccountKeyTaken)
			}
			return 0, fmt.Errorf("problem updating users: %w", err)
		}

		output.Queries = append(output.Queries, query)
		output.Logs = append(output.Logs, updatedRes.String())
	}

	// pgx encodes the structs as JSON for the json columns.
	var ID int
	if err := tx.QueryRow(ctx, `INSERT INTO operation_trace (input, output, revert, status, type)
		VALUES ($1, $2, $3, 'success', 'email_update') RETURNING id`,
		input, output, revert).Scan(&ID); err != nil {
		return 0, fmt.Errorf("problem creating operation_trace: %w", err)
	}

	return ID, tx.Commit(ctx)
}

// RevertOperation undoes the latest email_update for this email pair. The
// row is locked, so two reverts at once cannot both run.
func (o *OrdersDB) RevertOperation(ctx context.Context, newEmail string, oldEmail string) error {
	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("o.pool.Begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id     int
		status string
		input  emailInput
	)
	if err := tx.QueryRow(ctx, `SELECT id, status, input FROM operation_trace
		WHERE input->>'new_email'=$1 AND input->>'old_email'=$2
		ORDER BY id DESC LIMIT 1 FOR UPDATE`, newEmail, oldEmail).Scan(&id, &status, &input); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.ErrOperationNotFound
		}
		return fmt.Errorf("problem getting operation_trace: %w", err)
	}

	if status == "reverted" {
		return fmt.Errorf("operation already reverted")
	}

	// The statements come from the code, built from the stored input.
	revert := QueryLog{Queries: revertStatements(input), Logs: []string{}}
	for _, query := range revert.Queries {
		revertRes, err := tx.Exec(ctx, query.SQL, query.args()...)
		if err != nil {
			return fmt.Errorf("problem reverting operation: %w", err)
		}
		revert.Logs = append(revert.Logs, revertRes.String())
	}

	if _, err := tx.Exec(ctx, `UPDATE operation_trace SET revert = $1, status = 'reverted' WHERE id = $2`,
		revert, id); err != nil {
		return fmt.Errorf("problem updating operation_trace: %w", err)
	}

	return tx.Commit(ctx)
}
