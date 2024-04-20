package repo

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

func (o *OrdersDB) FlagOrdersToRenew(ctx context.Context, month int64, year int64) (int64, error) {
	// Select all unique individuals who have an active renewable order
	qOPotentialStr := `
	select userkey, count(userkey) as qt
	from orders where ("Status" = 'paid'
	or "Status" = 'nosuccess')
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
		select 
		id,
		"Type",
		"PaymentDate" from orders
		where userkey = $1
		and ("Status"='paid'
		or "Status"='nosuccess')
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

			if int64(aOSelect.PaymentDate.Time.Month()) >= month && int64(aOSelect.PaymentDate.Time.Year()) >= year {
				utils.LogFor(ctx).Info("No need to charge order", slog.Int("order_id", aOSelect.ID))
				continue
			}

			if aOSelect.Type.String == "regular" {
				utils.LogFor(ctx).Info("No need to charge regular order", slog.Int("order_id", aOSelect.ID))
				continue
			}

			// if not this month and not regular, go ahead
			utils.LogFor(ctx).Info("marking order for renewal", slog.Int("order_id", aOSelect.ID))
			err = o.flagOrder(ctx, aOSelect.ID, "torenew")
			if err != nil {
				return counter, fmt.Errorf("o.flagOrder: %w", err)
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

		if _, err := o.flagOrdersByAccountID(ctx, id, "duplicate"); err != nil {
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

		if err := o.flagOrder(ctx, id, flag); err != nil {
			return 0, fmt.Errorf("o.flagOrder: %w", err)
		}

		count++
	}
	return count, nil
}

func (o *OrdersDB) flagOrder(ctx context.Context, id int, flag string) error {
	_, err := o.Exec(ctx, `UPDATE orders SET "Flag" = $2 where id = $1`, id, flag)
	return err
}
