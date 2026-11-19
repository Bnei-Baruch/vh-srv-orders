package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// sqlQuery is one parameterized statement, kept in operation_trace so a
// revert can run it later with the same arguments.
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

	newKcId := req.NewKeycloakID
	oldKcId := req.OldKeycloakID
	newEmail := req.NewEmail
	oldEmail := req.OldEmail

	var output QueryLog
	var input emailInput
	var revert QueryLog

	var queryArr []sqlQuery

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// add records an update and the statement that undoes it.
	add := func(sql string, args, revertArgs []string) {
		queryArr = append(queryArr, sqlQuery{SQL: sql, Args: args})
		revert.Queries = append(revert.Queries, sqlQuery{SQL: sql, Args: revertArgs})
	}

	if oldKcId == nil {
		add(`UPDATE accounts SET "Email" = $1 WHERE "Email" = $2 AND "UserKey" = $3`,
			[]string{*newEmail, *oldEmail, *newKcId}, []string{*oldEmail, *newEmail, *newKcId})

		// Order Table: no need to update orders table because it has no relation to email
		// Pelecard Table: no need to update payments_pelecard table because it has no relation to email
		// Payments Table: no need to update payments table because it has no relation to email

		add(`UPDATE specials SET email = $1 WHERE email = $2`,
			[]string{*newEmail, *oldEmail}, []string{*oldEmail, *newEmail})

	} else {
		add(`UPDATE accounts SET "Email" = $1, "UserKey" = $2 WHERE "Email" = $3 AND "UserKey" = $4`,
			[]string{*newEmail, *newKcId, *oldEmail, *oldKcId}, []string{*oldEmail, *oldKcId, *newEmail, *newKcId})

		add(`UPDATE orders SET userkey = $1 WHERE userkey = $2`,
			[]string{*newKcId, *oldKcId}, []string{*oldKcId, *newKcId})

		add(`UPDATE payments_pelecard SET ord_key = $1 WHERE ord_key = $2`,
			[]string{*newKcId, *oldKcId}, []string{*oldKcId, *newKcId})

		add(`UPDATE payments SET "Ordkey" = $1 WHERE "Ordkey" = $2`,
			[]string{*newKcId, *oldKcId}, []string{*oldKcId, *newKcId})

		add(`UPDATE specials SET email = $1 WHERE email = $2`,
			[]string{*newEmail, *oldEmail}, []string{*oldEmail, *newEmail})

		add(`UPDATE specials_sep2021 SET email = $1 WHERE email = $2`,
			[]string{*newEmail, *oldEmail}, []string{*oldEmail, *newEmail})
	}

	input.NewEmail = newEmail
	input.NewKeycloakID = newKcId
	input.OldKeycloakID = oldKcId
	input.OldEmail = req.OldEmail

	// run loop for all queries
	for _, query := range queryArr {
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

	revert.Logs = []string{}

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
		revert QueryLog
	)
	if err := tx.QueryRow(ctx, `SELECT id, status, revert FROM operation_trace
		WHERE input->>'new_email'=$1 AND input->>'old_email'=$2
		ORDER BY id DESC LIMIT 1 FOR UPDATE`, newEmail, oldEmail).Scan(&id, &status, &revert); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.ErrOperationNotFound
		}
		return fmt.Errorf("problem getting operation_trace: %w", err)
	}

	if status == "reverted" {
		return fmt.Errorf("operation already reverted")
	}

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
