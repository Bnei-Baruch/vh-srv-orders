package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleCreateOrderAndPay(c *gin.Context) {
	var req repo.RequestOrder
	errRequest := c.BindJSON(&req)

	if errRequest != nil {
		log.Println("Err:", errRequest)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest})
		return
	}

	ord, errOrderCreation := o.repo.CreateOrder(c, req)

	if errOrderCreation != nil {
		log.Println("Err:", errOrderCreation)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errOrderCreation})
		return
	}

	p, errPaymentCreation := o.repo.CreatePayment(c, req, ord)

	if errPaymentCreation != nil {
		log.Println("Err:", errPaymentCreation)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errPaymentCreation})
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

	extPay := repo.RequestPayment{
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

	resp, err := utils.PostJSON("POST", ENDPOINT, payload)
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

			var serRes repo.OrderServiceEmvRes
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
