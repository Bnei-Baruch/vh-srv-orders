package main

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"gopkg.in/guregu/null.v4"
)

func getPaymentByEmail(ctx *gin.Context, email string) ([]PaymentByEmail, error) {

	paymentData := []PaymentByEmail{}

	rows, err := DB.Query(ctx, `select p.created_at, o."PaymentDate", o."Type", o."Amount", p."CCNumber", p."PaymentStatus"
	from payments as p, orders as o, accounts as a
	where a."Email" = $1
	and a.id = o."AccountID"
	and o.id = p."OrderID"
	order by o."PaymentDate" desc`, email)
	if err != nil {
		return paymentData, err
	}

	defer rows.Close()

	for rows.Next() {

		var p PaymentByEmail
		var amount string

		err := rows.Scan(&p.CreatedAt, &p.PaymentDate, &p.Type, &amount, &p.CCNumber, &p.PaymentStatus)

		if err != nil {
			return paymentData, err
		}

		value, err := strconv.ParseFloat(amount, 32)

		if err != nil {
			fmt.Println("error converting amount string to float")
			return paymentData, err
		}

		floatAmount := float64(value)

		p.Amount = null.NewFloat(floatAmount, true)

		paymentData = append(paymentData, p)
	}

	return paymentData, nil
}
