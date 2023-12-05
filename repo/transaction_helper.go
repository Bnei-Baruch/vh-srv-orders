package repo

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (o *OrdersDB) GetTransactionById(ctx context.Context, id int) (Transaction, error) {
	var (
		transaction Transaction
	)

	if err := o.QueryRow(ctx, `SELECT 
		id,
		order_id,
		payment_id,
		account_id,
		terminal_id,
		status,
		created_at,
		updated_at from transaction `+fmt.Sprintf("where id = %d", id)).Scan(
		&transaction.ID,
		&transaction.OrderID,
		&transaction.PaymentID,
		&transaction.AccountID,
		&transaction.TerminalID,
		&transaction.Status,
		&transaction.CreatedAt,
		&transaction.UpdatedAt,
	); err != nil {
		return transaction, err
	}
	return transaction, nil

}

func (o *OrdersDB) CreateTransactionAndGetId(ctx context.Context, p Transaction) (int, error) {

	createString, numString, createQueryArgs := prepareTransactionCreateQuery(p)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO transaction (%s) VALUES (%s) RETURNING id`, createString, numString),
			createQueryArgs...).Scan(
			&ID,
		); err != nil {
			return 0, err
		}
		return ID, nil
	} else {
		return 0, fmt.Errorf("invalid body")
	}

}

func prepareTransactionCreateQuery(req Transaction) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.AccountID.Valid {
		createStrings = append(createStrings, "account_id")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.AccountID.Int)
	}

	if req.OrderID.Valid {
		createStrings = append(createStrings, "order_id")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.OrderID.Int)
	}

	if req.PaymentID.Valid {
		createStrings = append(createStrings, "payment_id")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.PaymentID.Int)
	}

	if req.TerminalID.Valid {
		createStrings = append(createStrings, "terminal_id")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.TerminalID.String)
	}

	if req.Status.Valid {
		createStrings = append(createStrings, "status")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.Status.Int)
	}

	if len(args) != 0 {
		createStrings = append(createStrings, "created_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())

		createStrings = append(createStrings, "updated_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}
