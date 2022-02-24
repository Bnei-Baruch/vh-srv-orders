package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

//Status returns membership and status
func Status(c *gin.Context) {
	filter := string(c.Params.ByName("email"))
	paidmb := hasPaidMembership(c, filter)
	ticket := hasTicket(c, filter)
	specialmb := hasSpecialMembership(c, filter)

	var mb bool

	mbLabel := ""
	mbColor := ""

	if paidmb {
		mb = true
		mbLabel = "active"
		mbColor = "34A853"
		c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor})
		return
	}
	if specialmb {
		mb = true
		mbLabel = "active"
		mbColor = "2980b9"
		c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor})
		return
	}
	mb = false
	mbLabel = "inactive"
	mbColor = "5F6368"
	c.JSON(http.StatusOK, gin.H{"membership": mb, "ticket": ticket, "status_name": mbLabel, "status_color": mbColor})
	return

}

func hasPaidMembership(ctx *gin.Context, email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = $1
and o."AccountID" = a.id
and o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success' or o."Status" = 'nosuccess')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	if err := DB.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}

func hasTicket(ctx *gin.Context, email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = $1
and o."AccountID" = a.id
and (o."ProductType" = 'jan2022ticket' or
     o."ProductType" = 'jan2022ticket10' or
     o."ProductType" = 'jan2022ticket30')
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.Raw(query, email).Scan(&r)
	if err := DB.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}

func hasSpecialMembership(ctx *gin.Context, email string) bool {
	query := `
select count(s.*) as total
from specials as s
where s."email" = $1
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.Raw(query, email).Scan(&r)

	if err := DB.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}
