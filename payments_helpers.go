package main

import (
	"fmt"
	"strconv"
)

func countsAllPaymentsByMonth(filter string, month string) int64 {
	req := `select id  from payments
	where "PaymentStatus" = ? 
	 and date_part('month', "created_at") = ? `
	rows, err := DB.Raw(req, filter, month).Rows()

	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	for rows.Next() {
		rows.Scan(&id)
		fmt.Println(id)
		count++
	}
	return int64(count)

}

func countsAllPaymentsByMonthAndCurrency(filter string, month string, currency string) (int64, float32) {
	req := `select id, "Amount"  from payments 
	where "PaymentStatus" = ? 
	 and "DebitCurrency" = ?
	 and date_part('month', "created_at") = ? `
	rows, err := DB.Raw(req, filter, currency, month).Rows()

	if err != nil {
		return -1, -1
	}

	defer rows.Close()

	var count int
	var id int
	var sum float32
	var amount string
	count = 0
	sum = 0

	for rows.Next() {
		rows.Scan(&id, &amount)
		fmt.Println(id)

		af, _ := strconv.ParseFloat(amount, 32)

		sum = sum + float32(af)
		count++
	}
	return int64(count), sum

}
