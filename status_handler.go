package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

//Status: return membership and status
func Status(c *gin.Context) {
	filter := string(c.Params.ByName("email"))
	paidmb := hasPaidMembership(filter)
	ticket := hasTicket(filter)

	mb := paidmb

	mbLabel := ""
	mbColor := ""

	if mb {
		mbLabel = "active"
		mbColor = "34A853"
	} else {
		mbLabel = "inactive"
		mbColor = "5F6368"
	}

	c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor})
}

func hasSpecialMembership(email string) bool {
	return false
}

func hasPaidMembership(email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = ?
and o."AccountID" = a.id
and o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	DB.Raw(query, email).Scan(&r)

	return r.Total > 0
}

func hasTicket(email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = ?
and o."AccountID" = a.id
and o."ProductType" = 'sept2021ticket'
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	DB.Raw(query, email).Scan(&r)

	return r.Total > 0
}
