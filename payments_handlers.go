package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func handlePaymentCountByMonth(c *gin.Context) {
	var total int64
	filter := string(c.Params.ByName("filter"))
	month := string(c.Params.ByName("month"))
	//TODO check value of filter and month
	total = countsAllPaymentsByMonth(filter, month)
	c.JSON(http.StatusOK, gin.H{
		filter:  total,
		"month": month,
	})
}

func handlePaymentCountByMonthAndCurrency(c *gin.Context) {
	var total int64
	var sum float32
	sum = 0
	filter := string(c.Params.ByName("filter"))
	month := string(c.Params.ByName("month"))
	currency := string(c.Params.ByName("currency"))
	//TODO check value of filter and month
	total, sum = countsAllPaymentsByMonthAndCurrency(filter, month, currency)
	c.JSON(http.StatusOK, gin.H{
		filter:     total,
		"month":    month,
		"currency": currency,
		"sum":      sum,
	})
}
