package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Status returns membership and status
func (o *OrdersAPI) status(c *gin.Context) {
	filter := string(c.Params.ByName("email"))
	paidmb := o.repo.HasPaidMembership(c, filter)
	ticket := o.repo.HasTicket(c, filter)
	specialmb := o.repo.HasSpecialMembership(c, filter)

	var mb bool

	mbLabel := ""
	mbColor := ""

	if paidmb {
		mb = true
		mbLabel = "active"
		mbColor = "34A853"
		c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor, "is_special": false})
		return
	}
	if specialmb {
		mb = true
		mbLabel = "active"
		mbColor = "2980b9"
		c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor, "is_special": true})
		return
	}
	mb = false
	mbLabel = "inactive"
	mbColor = "5F6368"
	c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor, "is_special": false})
	return

}
