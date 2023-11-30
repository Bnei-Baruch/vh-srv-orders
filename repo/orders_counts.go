package repo

import (
	"context"
	"log"
)

func (o *OrdersDB) CountsAllOrders(ctx context.Context) int64 {
	var result int64

	if err := o.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(
		&result,
	); err != nil {
		log.Println(err)
		return 0
	}

	return result
}

func (o *OrdersDB) CountsFilteredOrders(ctx context.Context, filter string) int64 {

	var result int64

	if err := o.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE "Status"=$1`, filter).Scan(
		&result,
	); err != nil {
		log.Println(err)
		return 0
	}

	return result

}

func (o *OrdersDB) CountsTicketsOrders(ctx context.Context) int64 {
	query := `
select count(distinct o."AccountID") as total
from orders as o
where o."ProductType" = 'jan2022ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int64
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	if err := o.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func (o *OrdersDB) CountsConventionOrders(ctx context.Context) int64 {
	query := `
select count(o.*) as total
from orders as o
where o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success')
and o.created_at > '2021-09-03'
and (select count(q.id) from orders as q where q."AccountID" = o."AccountID" and o."Status" = 'paid') < 2
`
	type Results struct {
		Total int64
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	if err := o.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func (o *OrdersDB) CountsTickets10Orders(ctx context.Context) int64 {
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
	type Results struct {
		Total int64
	}

	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.Raw(query).Scan(&r)
	if err := o.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func (o *OrdersDB) CountsTickets30Orders(ctx context.Context) int64 {
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
	type Results struct {
		Total int64
	}

	var r Results

	if err := o.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.QueryRow(query).Scan(&r)

	return r.Total
}

func (o *OrdersDB) PaidDetailCount(ctx context.Context) PaidDetailC {

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

	var r PaidDetailC
	if fErr := o.QueryRow(ctx, totalPeoplePaid).Scan(
		&r.TotalPeoplePaid,
	); fErr != nil {
		return r
	}

	if sErr := o.QueryRow(ctx, totalPeoplePaidWithCC).Scan(
		&r.TotalPeoplePaidWithCC,
	); sErr != nil {
		return r
	}
	if tErr := o.QueryRow(ctx, totalTicketSold).Scan(
		&r.TotalTicketSold,
	); tErr != nil {
		return r
	}

	return r
}
