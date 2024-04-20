package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleOrdersCount(c *gin.Context) {
	var (
		total int64
		res   *repo.PaidDetailC
		err   error
	)

	filter := string(c.Params.ByName("filter"))
	switch filter {
	case "all":
		total, err = o.repo.CountsAllOrders(c.Request.Context())
	case "paid":
		total, err = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "failed":
		total, err = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "pending":
		total, err = o.repo.CountsFilteredOrders(c.Request.Context(), filter)
	case "tickets":
		total, err = o.repo.CountsTicketsOrders(c.Request.Context())
	case "tickets10":
		total, err = o.repo.CountsTickets10Orders(c.Request.Context())
	case "tickets30":
		total, err = o.repo.CountsTickets30Orders(c.Request.Context())
	case "convention":
		total, err = o.repo.CountsConventionOrders(c.Request.Context())
	// for event in may2022
	case "0522":
		res, err = o.repo.PaidDetailCount(c.Request.Context())
	default:
		total, err = o.repo.CountsAllOrders(c.Request.Context())
	}
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo count [%s]: %w", filter, err))
		return
	}

	if filter == "0522" {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": res, "success": true})
		return
	}

	c.JSON(http.StatusOK, gin.H{filter: total})
}
