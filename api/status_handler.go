package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Status returns membership and status
func (o *OrdersAPI) status(c *gin.Context) {
	filter := string(c.Params.ByName("email"))

	paidmb, err := o.repo.HasPaidMembership(c.Request.Context(), filter)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.HasPaidMembership: %w", err))
		return
	}

	ticket, err := o.repo.HasTicket(c.Request.Context(), filter)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.HasTicket: %w", err))
		return
	}

	specialmb, err := o.repo.HasSpecialMembership(c.Request.Context(), filter)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.HasSpecialMembership: %w", err))
		return
	}

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
