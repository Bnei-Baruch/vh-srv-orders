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

	// oneRow: the statement must update exactly one row, or the whole
	// operation is refused.
	oneRow bool
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
		q = append(q, sqlQuery{SQL: `UPDATE accounts SET "Email" = $1 WHERE "Email" = $2 AND "UserKey" = $3`,
			Args: []string{toEmail, fromEmail, toKc}, oneRow: true})
	} else {
		q = append(q,
			sqlQuery{SQL: `UPDATE accounts SET "Email" = $1, "UserKey" = $2 WHERE "Email" = $3 AND "UserKey" = $4`,
				Args: []string{toEmail, toKc, fromEmail, *fromKc}, oneRow: true},
			sqlQuery{SQL: `UPDATE orders SET userkey = $1 WHERE userkey = $2`, Args: []string{toKc, *fromKc}},
			sqlQuery{SQL: `UPDATE payments_pelecard SET ord_key = $1 WHERE ord_key = $2`, Args: []string{toKc, *fromKc}},
			sqlQuery{SQL: `UPDATE payments SET "Ordkey" = $1 WHERE "Ordkey" = $2`, Args: []string{toKc, *fromKc}},
		)
	}
	return append(q,
		sqlQuery{SQL: `UPDATE specials SET email = $1 WHERE email = $2`, Args: []string{toEmail, fromEmail}},
		sqlQuery{SQL: `UPDATE specials_sep2021 SET email = $1 WHERE email = $2`, Args: []string{toEmail, fromEmail}},
	)
}

// revertStatements undoes renameStatements for the same input.
func revertStatements(in emailInput) []sqlQuery {
	if in.OldKeycloakID == nil {
		return renameStatements(*in.NewEmail, *in.OldEmail, nil, *in.NewKeycloakID)
	}
	return renameStatements(*in.NewEmail, *in.OldEmail, in.NewKeycloakID, *in.OldKeycloakID)
}

// execStatements runs the statements in order and returns each result. The
// account statement must hit exactly one row: if the account no longer has
// the email and key the statements start from, nothing is changed.
func execStatements(ctx context.Context, tx pgx.Tx, qs []sqlQuery) ([]string, error) {
	logs := []string{}
	for _, q := range qs {
		res, err := tx.Exec(ctx, q.SQL, q.args()...)
		if err != nil {
			return nil, err
		}
		if q.oneRow && res.RowsAffected() != 1 {
			return nil, common.ErrOperationNoAccount
		}
		logs = append(logs, res.String())
	}
	return logs, nil
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
	revert := QueryLog{Queries: revertStatements(input), Logs: []string{}}

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rename := renameStatements(*input.OldEmail, *input.NewEmail, input.OldKeycloakID, *input.NewKeycloakID)
	logs, err := execStatements(ctx, tx, rename)
	if err != nil {
		// The new key already has an account: that is a merge, not a rename.
		if isKeyTaken(err) {
			return 0, fmt.Errorf("new keycloak id: %w; merge the accounts instead", common.ErrAccountKeyTaken)
		}
		if errors.Is(err, common.ErrOperationNoAccount) {
			return 0, err
		}
		return 0, fmt.Errorf("problem updating users: %w", err)
	}
	output := QueryLog{Queries: rename, Logs: logs}

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
		return common.ErrOperationReverted
	}

	// The statements come from the code, built from the stored input.
	revert := QueryLog{Queries: revertStatements(input)}
	if revert.Logs, err = execStatements(ctx, tx, revert.Queries); err != nil {
		// The old key has an account again (e.g. a login since the rename).
		if isKeyTaken(err) {
			return fmt.Errorf("old keycloak id: %w; merge the accounts instead", common.ErrAccountKeyTaken)
		}
		if errors.Is(err, common.ErrOperationNoAccount) {
			return err
		}
		return fmt.Errorf("problem reverting operation: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE operation_trace SET revert = $1, status = 'reverted' WHERE id = $2`,
		revert, id); err != nil {
		return fmt.Errorf("problem updating operation_trace: %w", err)
	}

	return tx.Commit(ctx)
}
