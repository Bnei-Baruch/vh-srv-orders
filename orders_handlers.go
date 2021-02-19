package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

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

func handleCreateOrderAndPay(c *gin.Context) {
	var req RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ord, err := createOrder(req)

	p, err := createPayment(req, ord)
	paramx := "mb-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX")
	ordkey := "ord-" + strconv.FormatUint(uint64(ord.ID), 10) + os.Getenv("SUFX")

	errorurl := req.ErrorURL + "/" + ordkey + "/" + paramx
	cancelurl := req.CancelURL + "/" + ordkey + "/" + paramx

	extPay := RequestPayment{
		UserKey: ordkey,

		GoodURL:   req.SuccessURL,
		ErrorURL:  errorurl,
		CancelURL: cancelurl,

		Name:         req.FirstName + " " + req.LastName,
		Price:        req.Amount,
		Currency:     req.Currency,
		Email:        req.Email,
		Phone:        "+NA",
		Street:       req.Street,
		City:         req.City,
		Country:      "Undef",
		Participans:  "1",
		Details:      req.Reference,
		SKU:          req.SKU,
		VAT:          "f",
		Installments: 1,
		Language:     req.OrderLanguage,
		Reference:    paramx,
		Organization: req.Organization,
	}

	fmt.Println(extPay)

	payload, err := json.Marshal(extPay)

	ENDPOINT := ""

	if req.Type == "recurring" {
		ENDPOINT = "https://checkout.kbb1.com/token/new"
	}

	if req.Type == "regular" {
		ENDPOINT = "https://checkout.kbb1.com/payments/new"
	}

	resp, err := postJSON("POST", ENDPOINT, payload)
	defer resp.Body.Close()
	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)

	parsableBody := string(body)
	fmt.Println("response URL:", parsableBody)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		// Grisha you should fix that one... seriously
		if req.Type == "regular" {
			// if req.Type is regular - endpoint return some ass-shit string
			// gota parse the m*fkr
			actualURL := strings.Split(parsableBody, "'")[1]
			c.JSON(http.StatusOK, gin.H{"url": actualURL})
		} else {
			var i interface{}
			json.Unmarshal(body, &i)
			c.JSON(http.StatusOK, i)
		}
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

	updateOrderAfterPayment(p)

	c.JSON(http.StatusOK, nil)
	return
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

func handleCreatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)
	DB.Create(&p)
	c.JSON(http.StatusOK, p)
}

func handleOrdersRenewByID(c *gin.Context) {
	id := string(c.Params.ByName("id"))
	oid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "wrong type"})
	}
	renewedStatus := renewOrder(uint(oid))
	c.JSON(http.StatusOK, gin.H{"renewedStatus": renewedStatus})
	return
}

func handleOrdersRenew(c *gin.Context) {
	month := string(c.Params.ByName("month"))
	m, err := strconv.ParseInt(month, 10, 64)
	if err != nil {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "wrong type"})
	}
	count := findOrdersToRenew(int(m))
	c.JSON(http.StatusOK, gin.H{"count": count})
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
	flag := string(c.Params.ByName("flag"))
	switch flag {
	case "duplicates":
		count := flagDuplicateOrders(flag)
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
