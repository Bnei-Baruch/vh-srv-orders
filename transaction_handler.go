package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"gopkg.in/guregu/null.v4"
)

func handleTransactionGetByID(ctx *gin.Context) {
	id := ctx.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	transaction, err := getTransactionById(ctx, intID)

	if err != nil {
		if err == pgx.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "Transaction not found"})
			return
		}
		fmt.Println("Error:", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": transaction, "success": true})
		return
	}
}

func handleTransactionOrderAndPay(c *gin.Context) {
	var req RequestOrder
	errRequest := c.BindJSON(&req)

	if errRequest != nil {
		log.Println("Err:", errRequest)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest})
		return
	}

	ord, errOrderCreation := createOrderViaTransaction(c, req)

	if errOrderCreation != nil {
		log.Println("Err:", errOrderCreation)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errOrderCreation})
		return
	}

	p, errPaymentCreation := createPayment(c, req, ord)

	if errPaymentCreation != nil {
		log.Println("Err:", errPaymentCreation)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errPaymentCreation})
		return
	}

	if req.TerminalId.String == "" {
		if p.PaymentType.String == "pelecard" {
			if strings.ToLower(req.Type.String) == "recurring" {
				req.TerminalId = null.StringFrom("ben_recurring_pelecard")
			} else {
				req.TerminalId = null.StringFrom("ben_regular_pelecard")
			}
		}

		if p.PaymentType.String == "helphaver" {
			req.TerminalId = null.StringFrom("ben_helphaver")
		}
		if p.PaymentType.String == "offline" {
			req.TerminalId = null.StringFrom("ben_offline")
		}
	}

	int64PayId := int64(p.ID)

	tran := Transaction{
		OrderID:    null.NewInt(ord.ID, true),
		AccountID:  ord.AccountID,
		PaymentID:  null.NewInt(int64PayId, true),
		TerminalID: req.TerminalId,
	}

	_, err := createTransactionAndGetId(c, tran)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	if (req.PaymentType.String == "offline" || req.PaymentType.String == "helphaver") && req.PaymentType.Valid {
		c.JSON(http.StatusCreated, gin.H{"Order": ord, "Payment": p})
		return
	}

	paramx := "m-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX")
	ordkey := "ord-" + strconv.FormatUint(uint64(ord.ID), 10) + os.Getenv("SUFX")

	errorurl := req.ErrorURL.String + "/" + ordkey + "/" + paramx
	cancelurl := req.CancelURL.String + "/" + ordkey + "/" + paramx

	extPay := RequestPayment{
		UserKey: ordkey,

		GoodURL:   req.SuccessURL.String,
		ErrorURL:  errorurl,
		CancelURL: cancelurl,

		Name:         req.FirstName.String + " " + req.LastName.String,
		Price:        req.Amount.Float64,
		Currency:     req.Currency.String,
		Email:        req.Email.String,
		Phone:        "+NA",
		Street:       req.Street.String,
		City:         req.City.String,
		Country:      "Undef",
		Participans:  "1",
		Details:      req.Reference.String,
		SKU:          req.SKU.String,
		VAT:          "f",
		Installments: 1,
		Language:     req.OrderLanguage.String,
		Reference:    paramx,
		Organization: req.Organization.String,
	}

	fmt.Println(extPay)

	payload, err := json.Marshal(extPay)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	ENDPOINT := ""

	if req.Type.String == "recurring" {
		ENDPOINT = "https://checkout.kbb1.com/token/new"
	}

	if req.Type.String == "regular" {
		ENDPOINT = "https://checkout.kbb1.com/emv/new"
	}

	if req.Reference.String == "testemv" {
		fmt.Println("EMV")
		ENDPOINT = "https://checkout.kbb1.com/emv/new"
	}
	fmt.Println(ENDPOINT)

	resp, err := postJSON("POST", ENDPOINT, payload)
	if err != nil {
		fmt.Println("Error wehn posting to ENDPOINT")
		fmt.Println(err)
		c.JSON(http.StatusOK, gin.H{"url": errorurl})
		return
	}
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
		if req.Type.String == "regular" {
			// if req.Type is regular - endpoint return some ass-shit string
			// gota parse the m*fkr

			var serRes OrderServiceEmvRes
			if err := json.Unmarshal(body, &serRes); err != nil {
				log.Println("Err while parsing https://checkout.kbb1.com/emv/new response", err)
			}

			if serRes.Status == "success" {
				// actualURL := strings.Split(serRes.URL, "'")[1]
				actualURL := serRes.URL
				c.JSON(http.StatusOK, gin.H{"url": actualURL})
			} else {
				fmt.Println("--error-in-https://checkout.kbb1.com/emv/new--")
				var i interface{}
				json.Unmarshal(body, &i)
				c.JSON(http.StatusOK, i)
			}

		} else {
			var i interface{}
			json.Unmarshal(body, &i)
			c.JSON(http.StatusOK, i)
		}
	}
}

func handleTransactionPaid(c *gin.Context) {
	var rp RequestPaid
	err := c.BindJSON(&rp)

	if err != nil {
		log.Println("Err:", err)
		c.JSON(http.StatusBadRequest, gin.H{"Error": err})
		return
	}

	if len(rp.UserKey.String) == 0 {
		log.Println("Err: No Order ID provided")
		log.Println(">> ParamX: " + rp.ParamX.String)
		c.JSON(http.StatusBadRequest, gin.H{"Error": "No order id provided in UserKey"})
		return
	}

	p, err := updatePayment(c, rp)

	if err != nil {
		// TODO : ask grisha to return more info on error
		c.JSON(http.StatusUnprocessableEntity, gin.H{"Error": err})
		return
	}

	o, err := updateOrderAfterPayment(c, p)

	if p.PaymentStatus.String == "success" && o.ProductType.String == "jan2022ticket" {
		log.Println("Synch with Registration")
		err := syncServiceRegistration(c, p, o)

		if err != nil {
			log.Println("we have an error")
			log.Println(err)
		}
	}
	c.JSON(http.StatusOK, nil)
}
