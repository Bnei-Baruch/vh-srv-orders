package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

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

func handleCreateOrder(c *gin.Context) {
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
	paramx := "mb-" + strconv.FormatUint(uint64(p.ID), 10) + Conf["SUFX"]
	ordkey := "ord-" + strconv.FormatUint(uint64(ord.ID), 10) + Conf["SUFX"]

	extPay := RequestPayment{
		UserKey: ordkey,

		GoodURL:   req.SuccessURL,
		ErrorURL:  req.ErrorURL,
		CancelURL: req.CancelURL,

		Name:         req.FirstName + " " + req.LastName,
		Price:        req.Amount,
		Currency:     req.Currency,
		Email:        req.Email,
		Phone:        "+NA",
		Street:       req.Street,
		City:         req.City,
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          req.SKU,
		VAT:          "f",
		Installments: 1,
		Language:     req.OrderLanguage,
		Reference:    paramx,
		Organization: "ben2",
	}

	payload, err := json.Marshal(extPay)
	resp, err := postJSON("POST", "https://checkout.kbb1.com/token/new", payload)
	//resp, err := postJSON("POST", "https://checkout.kbb1.com/payments/new", payload)
	defer resp.Body.Close()
	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	parsableBody := string(body)
	//actualURL := strings.Split(parsableBody, "'")[1]

	fmt.Println("response URL:", parsableBody)
	var i interface{}
	json.Unmarshal(body, &i)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		//c.JSON(http.StatusOK, gin.H{"url": actualURL})
		c.JSON(http.StatusOK, i)
	}
}

func handlePaid(c *gin.Context) {
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

func handleReccuringsProcess(c *gin.Context) {
	// getOrdersToProcess
	// for each orderToProcess processPayment and update Order Status
}

func handleCreatePayment(c *gin.Context) {
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
