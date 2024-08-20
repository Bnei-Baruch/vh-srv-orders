package repo

import (
	"context"
	"fmt"
	"time"
)

func (o *OrdersDB) GetMonthlyCharges(ctx context.Context, skip int, limit int) ([]*MonthlyCharge, error) {
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery := buildAndGetMonthlyChargeWhereQuery()

	rows, err := o.Query(ctx, `
		SELECT id, start_date, end_date, month, year, status, properties, created_at, updated_at
		FROM monthly_charge`+whereQuery+orderByQuery+limitOffsetString)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	charges := make([]*MonthlyCharge, 0)
	for rows.Next() {
		var c MonthlyCharge
		if err := rows.Scan(&c.ID, &c.StartDate, &c.EndDate, &c.Month, &c.Year, &c.Status, &c.Properties, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		charges = append(charges, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return charges, nil
}

func (o *OrdersDB) CreateMonthlyCharge(ctx context.Context, c *MonthlyCharge) (int, error) {
	var ID int

	if err := o.QueryRow(ctx, `INSERT INTO monthly_charge (start_date, end_date, month, year, status) 
	VALUES ($1,$2,$3,$4, $5) RETURNING id`, c.StartDate, c.EndDate, c.Month, c.Year, c.Status).
		Scan(&ID); err != nil {
		return 0, err
	}

	return ID, nil
}

func (o *OrdersDB) GetOrdersToCharge(ctx context.Context, year int, month int) ([]*OrderToCharge, error) {
	query := `
	with last_order as (
		select distinct on ("AccountID") *
		from orders
		where "ProductType" = 'globalmembership'
			and "Status" in ('paid', 'success', 'nosuccess', 'cancelled', 'cancelledFailed')
		order by "AccountID", "PaymentDate" desc)
	select lo.id, lo."Status", lo."Amount", lo."Currency", lo.card_details_id, a."UserKey", a."Email" 
	from last_order lo
	inner join accounts a on lo."AccountID" = a.id
	where lo."Type" = 'recurring' and "Status" in ('paid', 'nosuccess') and lo.created_at < $1;
`

	rows, err := o.Query(ctx, query, time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	orders := make([]*OrderToCharge, 0)
	for rows.Next() {
		var otc OrderToCharge
		if err := rows.Scan(&otc.ID, &otc.Status, &otc.Amount, &otc.Currency, &otc.CardDetailsId, &otc.UserKey, &otc.Email); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}

		orders = append(orders, &otc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return nil, nil
}

func buildAndGetMonthlyChargeWhereQuery() (string, string) {
	return "", " ORDER BY created_at desc"
}
