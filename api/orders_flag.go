package api

import (
	"fmt"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (o *OrdersAPI) handleOrdersFlag(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	type req struct {
		Flag  string `json:"flag"`
		Month int64  `json:"month"`
		Year  int64  `json:"year"`
	}

	var body req
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch body.Flag {
	case "torenew":
		count, err := o.repo.FlagOrdersToRenew(c.Request.Context(), body.Month, body.Year)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.FlagOrdersToRenew: %w", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	case "duplicates":
		count, err := o.repo.FlagDuplicateOrders(c.Request.Context(), body.Flag)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.FlagDuplicateOrders: %w", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "flag unknown"})
		return
	}
}
