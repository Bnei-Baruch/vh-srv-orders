package main

import (
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
	a."FirstName", a."LastName",
	o."OrderLanguage"
	from orders as o, accounts as a
	where o."AccountID" = a.id
	and o."Status" <> 'pending'
	and o."Type" = 'recurring'
	limit 100
	`

	type result struct {
		o Order
		a Account
	}
	rows, err := DB.Raw(req).Rows() // (*sql.Rows, error)

	if err != nil {
		c.JSON(http.StatusInternalServerError, nil)
	}

	defer rows.Close()

	var res result
	var allRes []result

	for rows.Next() {
		rows.Scan(&res.o.ID,
			&res.o.CreatedAt,
			&res.o.Amount,
			&res.o.Currency,
			&res.o.PaymentDate,
			&res.o.Status,
			&res.o.Flag,
			&res.a.FirstName, &res.a.LastName,
			&res.o.OrderLanguage)

		allRes = append(allRes, res)
	}

	c.JSON(http.StatusOK, gin.H{"data": allRes})
}

func handleAdminSubscriptionByID(c *gin.Context) {
	c.JSON(http.StatusOK, nil)
}
