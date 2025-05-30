package repo

import (
	"context"
	"fmt"
)

func (o *OrdersDB) CountsAllOrders(ctx context.Context) (int64, error) {
	return o.count(ctx, "SELECT COUNT(*) FROM orders")
}

func (o *OrdersDB) CountsFilteredOrders(ctx context.Context, filter string) (int64, error) {
	return o.count(ctx, `SELECT COUNT(*) FROM orders WHERE "Status"=$1`, filter)
}

func (o *OrdersDB) CountsTicketsOrders(ctx context.Context) (int64, error) {
	query := `
select count(distinct o."AccountID") as total
from orders as o
where o."ProductType" = 'jan2022ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	return o.count(ctx, query)
}

func (o *OrdersDB) CountsConventionOrders(ctx context.Context) (int64, error) {
	query := `
select count(o.*) as total
from orders as o
where o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success')
and o.created_at > '2021-09-03'
and (select count(q.id) from orders as q where q."AccountID" = o."AccountID" and o."Status" = 'paid') < 2
`
	return o.count(ctx, query)
}

func (o *OrdersDB) CountsTickets10Orders(ctx context.Context) (int64, error) {
	query := `
select count(distinct o."AccountID") as total
from orders as o
where o."ProductType" = 'jan2022ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
and (
  (o."Currency" = 'USD' and o."Amount" = '10')
  or
  (o."Currency" = 'NIS' and o."Amount" = '35')
  or
  (o."Currency" = 'EUR' and o."Amount" = '9')
)
`
	return o.count(ctx, query)
}

func (o *OrdersDB) CountsTickets30Orders(ctx context.Context) (int64, error) {
	query := `
select count(distinct o."AccountID") as total
from orders as o
where o."ProductType" = 'jan2022ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
and (
  (o."Currency" = 'USD' and o."Amount" = '30')
  or
  (o."Currency" = 'NIS' and o."Amount" = '100')
  or
  (o."Currency" = 'EUR' and o."Amount" = '25')
)
`
	return o.count(ctx, query)
}

func (o *OrdersDB) PaidDetailCount(ctx context.Context) (*PaidDetailC, error) {
	totalPeoplePaid := `select count(distinct "AccountID") 
	from orders where "ProductType" like 't-0522-%' and "Status" = 'paid'`

	totalPeoplePaidWithCC := `select count(distinct o."AccountID")
	from orders as o, payments as p
	where o."ProductType" like 't-0522-%'
	and o."Status" = 'paid'
	and o.id = p."OrderID"
	and p."PaymentType" = 'pelecard'`

	totalTicketSold := `select count(distinct "AccountID") 
	from orders where "ProductType" like 't-0522-%' and "Status" = 'paid'`

	var result PaidDetailC
	if err := o.QueryRow(ctx, totalPeoplePaid).Scan(&result.TotalPeoplePaid); err != nil {
		return nil, fmt.Errorf("totalPeoplePaid: %w", err)
	}

	if err := o.QueryRow(ctx, totalPeoplePaidWithCC).Scan(&result.TotalPeoplePaidWithCC); err != nil {
		return nil, fmt.Errorf("totalPeoplePaidWithCC: %w", err)
	}

	if err := o.QueryRow(ctx, totalTicketSold).Scan(&result.TotalTicketSold); err != nil {
		return nil, fmt.Errorf("totalTicketSold: %w", err)
	}

	return &result, nil
}

func (o *OrdersDB) count(ctx context.Context, query string, args ...interface{}) (int64, error) {
	var result int64
	if err := o.QueryRow(ctx, query, args...).Scan(&result); err != nil {
		return 0, err
	}
	return result, nil
}
