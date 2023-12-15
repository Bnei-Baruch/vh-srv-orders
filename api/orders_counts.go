package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleOrdersCount(c *gin.Context) {
	var total int64
	var res repo.PaidDetailC
	filter := string(c.Params.ByName("filter"))
	switch filter {
	case "all":
		total = o.repo.CountsAllOrders(c.Request.Context())
	case "paid":
		total = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "failed":
		total = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "pending":
		total = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "tickets":
		total = o.repo.CountsTicketsOrders(c.Request.Context())
	case "tickets10":
		total = o.repo.CountsTickets10Orders(c.Request.Context())
	case "tickets30":
		total = o.repo.CountsTickets30Orders(c.Request.Context())
	case "convention":
		total = o.repo.CountsConventionOrders(c.Request.Context())
	// for event in may2022
	case "0522":
		res = o.repo.PaidDetailCount(c.Request.Context())
	default:
		total = o.repo.CountsAllOrders(c.Request.Context())
	}

	if filter == "0522" {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": res, "success": true})
		return
	}

	fmt.Printf("\n>> Count %s : %d", filter, total)
	c.JSON(http.StatusOK, gin.H{filter: total})
}
