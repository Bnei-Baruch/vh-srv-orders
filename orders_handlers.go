package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

/**
TODO: add update route (POST) taking a JSON
TODO: change price and currency
TODO: Add log of operations
TODO: add field for end
TODO: add route has active order for account - yes / no / data
TODO: update muhlafim
TODO: add invoice
TODO: fix issues in DB
TODO: add payment method
TODO: clean data
**/

//TODO: Rewrite and merge with new & pay
func handleOrdersCreate(c *gin.Context) {
	var req RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ord, err := createOrder(c, req)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, ord)
	}
}

func handleOrdersPaid(c *gin.Context) {
	var rp RequestPaid
	err := c.BindJSON(&rp)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	p, err := updatePayment(rp)

	if err != nil {
		// TODO : ask grisha to return more info on error
		c.JSON(http.StatusUnprocessableEntity, gin.H{"Error": err})
		return
	}

	o, err := updateOrderAfterPayment(p)

	if p.PaymentStatus == "success" && o.ProductType == "jan2022ticket" {
		log.Println("Synch with Registration")
		err := syncServiceRegistration(p, o)

		if err != nil {
			log.Println("we have an error")
			log.Println(err)
		}
	}
	c.JSON(http.StatusOK, nil)
}

func handleOrdersRenew(c *gin.Context) {
	type req struct {
		User string `json:"user"`
		Key  string `json:"key"`
	}

	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	} else {
		if body.User == "admin" && (body.Key == "t" || body.Key == "e") {
			fmt.Printf("Renewing with key : %s\n", body.Key)
			count := chargeOrdersToRenew(body.Key)
			c.JSON(http.StatusOK, gin.H{"count": count})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"Error": "You are not allowed here"})
		}
	}
}
