package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (o *OrdersAPI) handleOrdersFlag(c *gin.Context) {
	type req struct {
		Flag  string `json:"flag"`
		Month int64  `json:"month"`
		Year  int64  `json:"year"`
	}

	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	}

	switch body.Flag {
	case "torenew":
		count := o.repo.FlagOrdersToRenew(c, body.Month, body.Year)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	case "duplicates":
		count := o.repo.FlagDuplicateOrders(c, body.Flag)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "flag unknown"})
		return
	}
}
