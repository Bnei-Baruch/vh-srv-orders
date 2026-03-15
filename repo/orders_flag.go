package repo

import (
	"context"
	"fmt"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func (o *OrdersDB) FlagOrdersToRenew(ctx context.Context, month int64, year int64) (int64, error) {
	// Select all unique individuals who have an active renewable order
	qOPotentialStr := `
	select userkey, count(userkey) as qt
	from orders where ("Status" = 'paid' or "Status" = 'nosuccess')
	and "ProductType" = 'globalmembership'
	group by userkey
	order by qt desc
	`

	rows, err := o.Query(ctx, qOPotentialStr)
	if err != nil {
		return 0, fmt.Errorf("o.Query [potential]: %w", err)
	}
	defer rows.Close()

	type qOPotential struct {
		Userkey *string
		Qt      *int64
	}

	var aOPotential qOPotential
	var counter int64 = 0
	for rows.Next() {
		err := rows.Scan(&aOPotential.Userkey, &aOPotential.Qt)
		if err != nil {
			return 0, fmt.Errorf("rows.Scan [potential]: %w", err)
		}

		qOSelectStr := `
		select id, "Type", "PaymentDate" from orders
		where userkey = $1
		and ("Status"='paid' or "Status"='nosuccess')
		and "ProductType" = 'globalmembership'
		order by "PaymentDate" desc
		limit 1
		`

		oselected, err := o.Query(ctx, qOSelectStr, *aOPotential.Userkey)
		if err != nil {
			return 0, fmt.Errorf("o.Query [selected]: %w", err)
		}
		defer oselected.Close()

		for oselected.Next() {
			var aOSelect Order
			err := oselected.Scan(&aOSelect.ID, &aOSelect.Type, &aOSelect.PaymentDate)
			if err != nil {
				return counter, fmt.Errorf("rows.Scan [selected]: %w", err)
			}

			// Check if payment date is in or after the billing month
			paymentDate := aOSelect.PaymentDate.Time
			billingDate := time.Date(int(year), time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			if !paymentDate.Before(billingDate) {
				continue
			}

			// we skip regular here instead of in the query for a reason.
			// Mostly for cases where the last order is regular but a user still has previous non-cancelled  recurring orders.
			if aOSelect.Type.String == "regular" {
				continue
			}

			// if not this month and not regular, go ahead
			err = o.FlagOrder(ctx, aOSelect.ID, common.OrderFlagToRenew)
			if err != nil {
				return counter, fmt.Errorf("o.FlagOrder: %w", err)
			}

			counter++
		}

		if err := oselected.Err(); err != nil {
			return counter, fmt.Errorf("rows.Err [selected]: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return counter, fmt.Errorf("rows.Err [potential]: %w", err)
	}

	return counter, nil
}

func (o *OrdersDB) FlagDuplicateOrders(ctx context.Context, ProductType string) (int, error) {
	req := `select "AccountID" as id, count(*) as "duplicate" 
from orders where "Status" = 'paid' 
group by "AccountID" 
having count(*) > 1
order by duplicate desc`

	rows, err := o.Query(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var count int
	var id int
	count = 0
	var b int
	for rows.Next() {
		if err := rows.Scan(&id, &b); err != nil {
			return 0, fmt.Errorf("rows.Scan: %w", err)
		}

		if _, err := o.flagOrdersByAccountID(ctx, id, common.OrderFlagDuplicate); err != nil {
			return 0, fmt.Errorf("o.flagOrdersByAccountID: %w", err)
		}

		count++
	}

	return count, nil
}

func (o *OrdersDB) flagOrdersByAccountID(ctx context.Context, aid int, flag string) (int, error) {
	req := `select id from orders where "AccountID" = $1 and "Status" = 'paid'`
	rows, err := o.Query(ctx, req, aid)
	if err != nil {
		return 0, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var count int
	var id int
	count = 0
	for rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("rows.Scan: %w", err)
		}

		if err := o.FlagOrder(ctx, id, flag); err != nil {
			return 0, fmt.Errorf("o.FlagOrder: %w", err)
		}

		count++
	}
	return count, nil
}

func (o *OrdersDB) FlagOrder(ctx context.Context, id int, flag string) error {
	_, err := o.Exec(ctx, `UPDATE orders SET "Flag" = $2 where id = $1`, id, flag)
	return err
}

func (o *OrdersDB) FlagOrderAsRenewed(ctx context.Context, orderID uint) error {
	res, err := o.Exec(ctx, `UPDATE orders SET "Flag"=$1, updated_at=$2 WHERE id = $3`, common.OrderFlagRenewed, time.Now(), orderID)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}
	return nil
}

// GetFlaggedOrders returns all orders with Flag='torenew'
// Returns a slice of orders with id, Flag, and AccountID
func (o *OrdersDB) GetFlaggedOrders(ctx context.Context) ([]Order, error) {
	query := `
		SELECT id, "Flag", "AccountID", "Status"
		FROM orders
		WHERE "Flag" = $1
	`

	rows, err := o.Query(ctx, query, common.OrderFlagToRenew)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		if err := rows.Scan(&order.ID, &order.Flag, &order.AccountID, &order.Status); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		orders = append(orders, order)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return orders, nil
}

func (o *OrdersDB) GetOrderIDsToRenew(ctx context.Context) ([]uint, error) {
	sqlQuery := `
	SELECT id FROM orders 
	WHERE ("Status" = $1 OR "Status" = $2)
	AND "Type" = 'recurring'
	AND "Flag" = $3
	`

	rows, err := o.Query(ctx, sqlQuery, common.OrderStatusPaid, common.OrderStatusNoSuccess, common.OrderFlagToRenew)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var orderIDs []uint
	for rows.Next() {
		var id uint
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		orderIDs = append(orderIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return orderIDs, nil
}

// ClearAllFlags clears all order flags
// Note: We are not filtering by Flag here, so all flags will be cleared. On all orders.
// This is intentional as all flags are billing related and should be cleared on all orders.
func (o *OrdersDB) ClearAllFlags(ctx context.Context) error {
	_, err := o.Exec(ctx, `UPDATE orders SET "Flag" = ''`)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	// No need to check RowsAffected() as zero rows affected is not an error.
	return nil
}
