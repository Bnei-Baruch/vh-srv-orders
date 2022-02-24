package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleOrdersCount(c *gin.Context) {
	var total int64
	filter := string(c.Params.ByName("filter"))
	switch filter {
	case "all":
		total = countsAllOrders()
	case "paid":
		total = countsFilteredOrders(filter)
	case "failed":
		total = countsFilteredOrders(filter)
	case "pending":
		total = countsFilteredOrders(filter)
	case "tickets":
		total = countsTicketsOrders(c)
	case "tickets10":
		total = countsTickets10Orders(c)
	case "tickets30":
		total = countsTickets30Orders(c)
	case "convention":
		total = countsConventionOrders(c)
	default:
		total = countsAllOrders()
	}
	fmt.Printf("\n>> Count %s : %d", filter, total)
	c.JSON(http.StatusOK, gin.H{filter: total})
}

func countsTicketsOrders(ctx *gin.Context) int64 {
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
	if err := DB.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func countsConventionOrders(ctx *gin.Context) int64 {
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
	if err := DB.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func countsTickets10Orders(ctx *gin.Context) int64 {
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
	if err := DB.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}

	return r.Total
}

func countsTickets30Orders(ctx *gin.Context) int64 {
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

	if err := DB.QueryRow(ctx, query).Scan(
		&r.Total,
	); err != nil {
		return 0
	}
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.QueryRow(query).Scan(&r)

	return r.Total
}
