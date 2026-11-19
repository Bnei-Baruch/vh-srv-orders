package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

func (o *OrdersDB) GetTransactionById(ctx context.Context, id int, accountId *int) (*Transaction, error) {
	var transaction Transaction

	var args queryArgs
	whereCondition := "where id = " + args.arg(id)
	if accountId != nil {
		whereCondition += " and account_id = " + args.arg(*accountId)
	}
	if err := o.pool.QueryRow(ctx, `SELECT 
		id,
		order_id,
		payment_id,
		account_id,
		terminal_id,
		status,
		created_at,
		updated_at from transaction `+whereCondition, args...).Scan(
		&transaction.ID,
		&transaction.OrderID,
		&transaction.PaymentID,
		&transaction.AccountID,
		&transaction.TerminalID,
		&transaction.Status,
		&transaction.CreatedAt,
		&transaction.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &transaction, nil

}

func (o *OrdersDB) CreateTransactionAndGetId(ctx context.Context, p Transaction) (int, error) {
	createString, numString, createQueryArgs := prepareTransactionCreateQuery(p)
	if len(createQueryArgs) == 0 {
		return 0, common.ErrInvalidValues
	}

	var ID int
	if err := o.pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO transaction (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&ID); err != nil {
		return 0, err
	}
	return ID, nil
}

func prepareTransactionCreateQuery(req Transaction) (string, string, queryArgs) {
	var c colVals

	if req.AccountID.Valid {
		c.add("account_id", req.AccountID.Int)
	}

	if req.OrderID.Valid {
		c.add("order_id", req.OrderID.Int)
	}

	if req.PaymentID.Valid {
		c.add("payment_id", req.PaymentID.Int)
	}

	if req.TerminalID.Valid {
		c.add("terminal_id", req.TerminalID.String)
	}

	if req.Status.Valid {
		c.add("status", req.Status.Int)
	}

	if !c.empty() {
		c.add("created_at", time.Now())
		c.add("updated_at", time.Now())
	}

	return c.insertLists()
}
