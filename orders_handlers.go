package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

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

func handleOrdersList(c *gin.Context) {
	var o []Order
	if err := DB.Find(&o).Error; err != nil {
		c.AbortWithStatus(404)
		log.Println(err)
	} else {
		c.JSON(http.StatusOK, o)
	}
}

func handleOrdersCount(c *gin.Context) {
	var total int64
	filter := string(c.Params.ByName("filter"))
	switch filter {
	case "all":
		total = countsAllOrders()
	case "paid":
		total = countsFilteredOrders(filter)
	case "failed":
		total = countsFilteredOrders(filter)
	case "pending":
		total = countsFilteredOrders(filter)
	case "tickets":
		total = countsTicketsOrders()
	case "convention":
		total = countsConventionOrders()
	default:
		total = countsAllOrders()
	}

	c.JSON(http.StatusOK, gin.H{filter: total})
}

func handleOrdersCountByMonth(c *gin.Context) {
	var total int64
	filter := string(c.Params.ByName("filter"))
	month := string(c.Params.ByName("month"))
	//TODO check value of filter and month
	total = countsAllOrdersByMonth(filter, month)
	c.JSON(http.StatusOK, gin.H{
		filter:  total,
		"month": month,
	})
}

func handleOrdersCountByMonthAndCurrency(c *gin.Context) {
	var total int64
	var sum float32
	sum = 0
	filter := string(c.Params.ByName("filter"))
	month := string(c.Params.ByName("month"))
	currency := string(c.Params.ByName("currency"))
	//TODO check value of filter and month
	total, sum = countsAllOrdersByMonthAndCurrency(filter, month, currency)
	c.JSON(http.StatusOK, gin.H{
		filter:     total,
		"month":    month,
		"currency": currency,
		"sum":      sum,
	})
}

//TODO: Rewrite and merge with new & pay
func handleOrdersCreate(c *gin.Context) {
	var req RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ord, err := createOrder(req)

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
		//createOrphanPayment(rp)
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

func handleOrdersUpdateStatus(c *gin.Context) {
	status := string(c.Params.ByName("status"))
	id := string(c.Params.ByName("id"))
	oid, _ := strconv.ParseUint(id, 10, 64)

	o := getOrderByID(uint(oid))
	o.Status = status
	DB.Model(&o).Updates(o)

	c.JSON(http.StatusOK, o)
	return
}

func handleOrdersRenewByID(c *gin.Context) {
	id := string(c.Params.ByName("id"))
	oid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "wrong type"})
	}
	renewedStatus := renewOrder(uint(oid), "t")
	c.JSON(http.StatusOK, gin.H{"renewedStatus": renewedStatus})
	return
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
	return
}

func handleOrdersAnnotate(c *gin.Context) {
	note := string(c.Params.ByName("note"))
	id := string(c.Params.ByName("id"))
	oid, _ := strconv.ParseUint(id, 10, 64)

	addNoteToOrder(uint(oid), note)
	c.JSON(http.StatusOK, nil)
}

func handleOrdersFlag(c *gin.Context) {
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
		count := flagOrdersToRenew(body.Month, body.Year)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	case "duplicates":
		count := flagDuplicateOrders(body.Flag)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "flag unknown"})
		return
	}
}

func handleOrdersTest(c *gin.Context) {
	//renewOrder(5728)

	//c.JSON(http.StatusOK, gin.H{"account": val})
	//count := findOrdersToRenew(6)
	//fmt.Println(count)
	c.JSON(http.StatusOK, gin.H{"test": true})
	//return
}

func handleOrdersClean(c *gin.Context) {
	month := string(c.Params.ByName("month"))
	dups, _ := GetAccountsWithDuplicatesByMonth(month)

	for _, d := range dups {
		cleanDuplicates(d, month)
	}

	c.JSON(http.StatusOK, gin.H{"cleaned": len(dups)})
}
func handleVHisPaid(c *gin.Context) {
	keycloakID := string(c.Params.ByName("id"))
	total := activeOrderByKeycloakID(keycloakID)
	ispaid := false
	if total > 0 {
		ispaid = true
	}
	c.JSON(http.StatusOK,
		gin.H{"keycloakID": keycloakID,
			"total": total, "ispaid": ispaid})
}
