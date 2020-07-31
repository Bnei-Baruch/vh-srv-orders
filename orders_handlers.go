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

func handleCount(c *gin.Context) {
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

func handlePelecardStatus(c *gin.Context) {
	status := string(c.Params.ByName("status"))
	switch status {
	case "good":
		c.String(http.StatusOK, "Good")
	case "error":
		c.String(http.StatusOK, "Error")
	case "cancel":
		c.String(http.StatusOK, "Canceled")
	default:
		c.String(http.StatusNotFound, "Status: %v", status)
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

func handleUpdateOrderStatus(c *gin.Context) {
	status := string(c.Params.ByName("status"))
	id := string(c.Params.ByName("id"))
	oid, _ := strconv.ParseUint(id, 10, 64)

	o := getOrderByID(uint(oid))
	o.Status = status
	DB.Model(&o).Updates(o)

	c.JSON(http.StatusOK, o)
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

func handleRenew(c *gin.Context) {
	month := string(c.Params.ByName("month"))
	m, err := strconv.ParseInt(month, 10, 64)
	if err != nil {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "wrong type"})
	}
	count := findOrdersToRenew(int(m))
	c.JSON(http.StatusOK, gin.H{"count": count})
	return
}

func handleAnnotate(c *gin.Context) {
	note := string(c.Params.ByName("note"))
	id := string(c.Params.ByName("id"))
	oid, _ := strconv.ParseUint(id, 10, 64)

	addNoteToOrder(uint(oid), note)
	c.JSON(http.StatusOK, nil)
}

func handleFlag(c *gin.Context) {
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

func handleTest(c *gin.Context) {
	// renewOrder(4)
	// c.JSON(http.StatusOK, gin.H{"count": 0})
	count := findOrdersToRenew(6)
	fmt.Println(count)
	c.JSON(http.StatusOK, gin.H{"count": count})
	return
}
