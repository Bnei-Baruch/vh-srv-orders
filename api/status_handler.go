package api

import (
	"fmt"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Status returns membership and status
func (o *OrdersAPI) status(c *gin.Context) {
	email := c.Param("email")
	if !o.isEmailOwnerOrHasAnyRole(c, email, common.RoleAdmin, common.RoleRoot) {
		c.Status(http.StatusForbidden)
		return
	}
	paidmb, err := o.repo.HasPaidMembership(c.Request.Context(), email)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.HasPaidMembership: %w", err))
		return
	}

	ticket, err := o.repo.HasTicket(c.Request.Context(), email)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.HasTicket: %w", err))
		return
	}

	specialmb, err := o.repo.HasSpecialMembership(c.Request.Context(), email)
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
