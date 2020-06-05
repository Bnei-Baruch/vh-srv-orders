package main

import (
	"encoding/json"
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
	var o Order
	err := c.BindJSON(&o)

	if err != nil {
		panic(err.Error())
	}

	DB.Create(&o)
	c.JSON(http.StatusOK, o)
}

func createOrderAndPay(c *gin.Context) {
	var o Order
	c.BindJSON(&o)
	DB.Create(&o)

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
