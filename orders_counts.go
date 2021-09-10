package main

func countsTicketsOrders() int64 {
	query := `
select count(o.*) as total
from orders as o
where o."ProductType" = 'sept2021ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int64
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	DB.Raw(query).Scan(&r)

	return r.Total
}

func countsConventionOrders() int64 {
	query := `
select count(o.*) as total
from orders as o
where o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int64
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	DB.Raw(query).Scan(&r)

	return r.Total
}
