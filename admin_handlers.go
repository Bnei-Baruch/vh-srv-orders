package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleAdmin(c *gin.Context) {
	c.JSON(http.StatusOK, nil)
}

func handleAdminSubscriptions(c *gin.Context) {
	req := ` select
	o.id,
	o.created_at,
	o."Amount",
	o."Currency",
	o."PaymentDate",
	o."Status",
	o."Flag",
	a."FirstName", 
	a."LastName",
	o."OrderLanguage"
	from orders as o, accounts as a
	where o."AccountID" = a.id
	and o."Status" <> 'pending'
	and o."Type" = 'recurring'
	limit 100
	`

	type result struct {
		OrderInfo   Order
		AccountInfo Account
	}
	rows, err := DB.Raw(req).Rows() // (*sql.Rows, error)

	if err != nil {
		c.JSON(http.StatusInternalServerError, nil)
	}

	defer rows.Close()

	var res result
	var allRes []result

	for rows.Next() {
		rows.Scan(&res.OrderInfo.ID,
			&res.OrderInfo.CreatedAt,
			&res.OrderInfo.Amount,
			&res.OrderInfo.Currency,
			&res.OrderInfo.PaymentDate,
			&res.OrderInfo.Status,
			&res.OrderInfo.Flag,
			&res.AccountInfo.FirstName,
			&res.AccountInfo.LastName,
			&res.OrderInfo.OrderLanguage)

		allRes = append(allRes, res)
	}

	fmt.Println(allRes)

	c.JSON(http.StatusOK, allRes)
}

func handleAdminSubscriptionByID(c *gin.Context) {
	c.JSON(http.StatusOK, nil)
}
