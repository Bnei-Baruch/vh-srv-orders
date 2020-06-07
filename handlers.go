package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func listOrders(c *gin.Context) {
	var o []Order
	if err := DB.Find(&o).Error; err != nil {
		c.AbortWithStatus(404)
		log.Println(err)
	} else {
		c.JSON(http.StatusOK, o)
	}
}

func createOrder(c *gin.Context) {
	var req RequestOrder
	err := c.BindJSON(&req)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	o := Order{
		Type:          req.Type,
		ProductType:   req.ProductType,
		RecuringFreq:  req.RecurringFreq,
		Organization:  req.Organization,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        "pending",
		OrderLanguage: req.OrderLanguage,
	}

	fmt.Println(req)
	fmt.Println(o)

	//	DB.Create(&o)
	c.JSON(http.StatusOK, req)
}

func createOrderAndPay(c *gin.Context) {
	var req RequestOrder
	c.BindJSON(&req)
	//DB.Create(&req)

	var httpclient = &http.Client{Timeout: 10 * time.Second}
	url := "https://checkout.kbb1.com/payments/new"
	r, err := httpclient.Get(url)

	if err != nil {
		panic(err.Error())
	}

	defer r.Body.Close()

	var res interface{}
	json.NewDecoder(r.Body).Decode(res)

	c.JSON(http.StatusOK, res)
}

func createPayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)
	DB.Create(&p)
	c.JSON(http.StatusOK, p)
}

func createInvoice(c *gin.Context) {
	var i Invoice
	c.BindJSON(&i)
	DB.Create(&i)
	c.JSON(http.StatusOK, i)
}

func optionsHandler(c *gin.Context) {
	c.JSON(http.StatusNoContent, nil)
}
