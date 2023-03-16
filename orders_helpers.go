package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"gopkg.in/guregu/null.v4"
)

func countsAllOrders(c *gin.Context) int64 {
	var result int64

	if err := DB.QueryRow(c, "SELECT COUNT(*) FROM orders").Scan(
		&result,
	); err != nil {
		log.Println(err)
		return 0
	}

	return result
}

func countsFilteredOrders(c *gin.Context, filter string) int64 {

	var result int64

	if err := DB.QueryRow(c, `SELECT COUNT(*) FROM orders WHERE "Status"=$1`, filter).Scan(
		&result,
	); err != nil {
		log.Println(err)
		return 0
	}

	return result

}

func updateOrderStatusByOrderID(c *gin.Context, oid int64, status string) error {
	if _, err := DB.Exec(c, `UPDATE orders SET "Status"=$1 WHERE id=$2`, status, oid); err != nil {
		return err
	}
	return nil
}

func createOrder(c *gin.Context, req RequestOrder) (Order, error) {

	order_status := "pending"
	var account_id int64 = 0

	o := Order{
		Type:          req.Type,
		ProductType:   req.ProductType,
		RecuringFreq:  req.RecurringFreq,
		Organization:  req.Organization,
		Amount:        req.Amount,
		SKU:           req.SKU,
		Currency:      req.Currency,
		Status:        null.NewString(order_status, true),
		OrderLanguage: req.OrderLanguage,
		AccountID:     null.NewInt(account_id, true),
	}

	accountType := "personal"

	a := Account{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Street:    req.Street,
		City:      req.City,
		State:     req.State,
		Postcode:  req.Postcode,
		Country:   req.Country,

		AccountType: null.NewString(accountType, true),
		UserKey:     req.UserKey,
	}

	accountID := CreateOrUpdateAccount(c, a)

	if accountID == 0 {
		return o, errors.New("null account")
	}

	o.AccountID = null.NewInt(accountID, true)

	createString, numString, createQueryArgs := prepareOrderCreateQuery(o)

	if err := DB.QueryRow(c, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&o.ID,
	); err != nil {
		if err == pgx.ErrNoRows {
			return o, fmt.Errorf("no rows affected")
		}
		return o, err
	}

	return o, nil
}
func createOrderViaTransaction(c *gin.Context, req RequestOrder) (Order, error) {

	order_status := "pending"
	var account_id int64 = 0

	o := Order{
		Type:          req.Type,
		ProductType:   req.ProductType,
		RecuringFreq:  req.RecurringFreq,
		Organization:  req.Organization,
		Amount:        req.Amount,
		SKU:           req.SKU,
		Currency:      req.Currency,
		Quantity:      req.Quantity,
		AmountItem:    req.AmountItem,
		Status:        null.NewString(order_status, true),
		OrderLanguage: req.OrderLanguage,
		AccountID:     null.NewInt(account_id, true),
	}

	accountType := "personal"

	a := Account{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Street:    req.Street,
		City:      req.City,
		State:     req.State,
		Postcode:  req.Postcode,
		Country:   req.Country,

		AccountType: null.NewString(accountType, true),
		UserKey:     req.UserKey,
	}

	accountID := CreateOrUpdateAccount(c, a)

	if accountID == 0 {
		return o, errors.New("null account")
	}

	o.AccountID = null.NewInt(accountID, true)

	createString, numString, createQueryArgs := prepareOrderCreateQuery(o)

	if err := DB.QueryRow(c, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&o.ID,
	); err != nil {
		if err == pgx.ErrNoRows {
			return o, fmt.Errorf("no rows affected")
		}
		return o, err
	}

	return o, nil
}

func postJSON(method string, url string, payload []byte) (*http.Response, error) {
	fmt.Println("POSTING TO ENDPOINT")
	payReq, _ := http.NewRequest(method, url, bytes.NewReader(payload))
	payReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(payReq)
	if err != nil {
		return nil, err
	}
	return resp, err

}

func createPayment(c *gin.Context, req RequestOrder, o Order) (Payment, error) {

	payment_status := "pending"
	payment_type := "pelecard"

	if req.PaymentType.String == "offline" && req.PaymentType.Valid {
		payment_type = "offline"
	}
	if req.PaymentType.String == "helphaver" && req.PaymentType.Valid {
		payment_type = "helphaver"
	}

	p := Payment{
		Amount:        req.Amount,
		PaymentType:   null.NewString(payment_type, true),
		OrderID:       null.NewInt(o.ID, true),
		PaymentStatus: null.NewString(payment_status, true),
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)

	if err := DB.QueryRow(c, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&p.ID,
	); err != nil {
		return p, err
	}

	if req.PaymentType.String == "offline" && req.PaymentType.Valid {
		offlinePaymentErr := createOfflinePayment(c, req, p.ID, payment_status)

		if offlinePaymentErr != nil {
			return p, offlinePaymentErr
		}
	}
	if req.PaymentType.String == "helphaver" && req.PaymentType.Valid {
		offlinePaymentErr := createHelpHaverPayment(c, req, p.ID, payment_status)

		if offlinePaymentErr != nil {
			return p, offlinePaymentErr
		}
	}

	if payment_type == "pelecard" {
		pelecardPaymentErr := createPelecardPayment(c, req, p.ID, p)

		if pelecardPaymentErr != nil {
			return p, pelecardPaymentErr
		}
	}

	return p, nil

}

func createPendingPayment(c *gin.Context, sum null.Float, oid int64, pmx null.String) (Payment, error) {

	p := Payment{
		Amount:        sum,
		PaymentType:   null.NewString("pelecard", true),
		OrderID:       null.NewInt(oid, true),
		PaymentStatus: null.NewString("pending", true),
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)

	// Add new account if not exist
	if err := DB.QueryRow(c, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&p.ID,
	); err != nil {
		return p, err
	}

	createPelecardString, numPelecardString, createPelecardQueryArgs := preparePelecardPaymentCreateQuery(p, p.ID)

	_, err := DB.Exec(c, fmt.Sprintf(`INSERT INTO payments_pelecard (%s) VALUES (%s)`, createPelecardString, numPelecardString),
		createPelecardQueryArgs...)
	if err != nil {
		return p, err
	}

	paramx := "m-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX") + pmx.String
	ordkey := "ord-" + strconv.FormatUint(uint64(oid), 10) + os.Getenv("SUFX")
	fmt.Printf(">>>> ParamX: %s\n", paramx)

	p.ParamX = null.NewString(paramx, true)
	p.Ordkey = null.NewString(ordkey, true)

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, p.ID),
			toUpdateArgs...)
		if err != nil {
			fmt.Println("problem updating payment: %w", err)
			return p, err
		}

		// convert uint to int
		paymentId := int(p.ID)

		// Payment Struct to PaymentUpdate Struct
		pu := PaymentUpdate{
			PaymentID:     null.NewInt(int64(paymentId), true),
			Amount:        p.Amount,
			PaymentType:   p.PaymentType,
			OrderID:       p.OrderID,
			PaymentStatus: p.PaymentStatus,
			ParamX:        p.ParamX,
			Ordkey:        p.Ordkey,
		}

		toUpdatePelecard, toUpdatePelecardArgs := preparePelecardPaymentUpdateQuery(pu)

		if len(toUpdatePelecardArgs) != 0 {
			_, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, paymentId),
				toUpdatePelecardArgs...)
			if err != nil {
				fmt.Println("problem updating payments_pelecard: %w", err)
				return p, err
			}
		}

		if updateRes.RowsAffected() == 0 {
			return p, fmt.Errorf("no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}

	return p, nil
}

func updatePayment(ctx *gin.Context, req RequestPaid) (Payment, error) {
	var p Payment

	if len(req.Error.String) > 0 {
		return p, errors.New(req.Error.String)
	}

	orderid, err := strconv.ParseUint(strings.Split(req.UserKey.String, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}
	paymentid, err := strconv.ParseUint(strings.Split(req.ParamX.String, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}

	// Get payment
	if err := DB.QueryRow(ctx, `SELECT 
	"OrderID",
	"PaymentStatus",
	"PaymentType",
	"ParamX",
	"AuthNo",
	confirmation_key,
	success,
	pelecard_token,
	"TransactionID",
	"CCBrand",
	"CardHebrewName",
	"CCAbroadCard",
	"CCCompanyClearer",
	credit_type,
	"CCExpDate",
	"CCNumber",
	"DebitCode",
	"DebitCurrency",
	"DebitTotal",
	"DebitType",
	"FirstPaymentTotal",
	"FixedPaymentTotal",
	"TotalPayments",
	j_param,
	"TransactionInitTime",
	"TransactionUpdateTime",
	"VoucherID" FROM payments WHERE "OrderID"=$1 AND id=$2`, uint(orderid), uint(paymentid)).Scan(
		&p.OrderID, &p.PaymentStatus, &p.PaymentType, &p.ParamX, &p.AuthNo, &p.ConfirmationKey, &p.Success,
		&p.PelecardToken, &p.TransactionID, &p.CCBrand, &p.CardHebrewName, &p.CCAbroadCard,
		&p.CCCompanyClearer, &p.CreditType, &p.CCExpDate, &p.CCNumber, &p.DebitCode, &p.DebitCurrency,
		&p.DebitTotal, &p.DebitType, &p.FirstPaymentTotal, &p.FixedPaymentTotal, &p.TotalPayments,
		&p.JParam, &p.TransactionInitTime, &p.TransactionUpdateTime, &p.VoucherID,
	); err != nil {
		if err == pgx.ErrNoRows {
			return p, errors.New("cannot find related Order for Payment")
		}
	}

	//update payment object
	if req.Success.String == "1" {
		p.PaymentStatus = null.NewString("success", true)
		p.PaymentType = null.NewString("pelecard", true)
		p.ParamX = req.ParamX
		p.AuthNo = req.AuthNo
		p.ConfirmationKey = req.ConfirmationKey
		p.Success = req.Success
		p.PelecardToken = req.Token
		p.TransactionID = req.TransactionID
		p.CCBrand = req.CCBrand
		p.CardHebrewName = req.CardHebrewName
		p.CCAbroadCard = req.CCAbroadCard
		p.CCCompanyClearer = req.CCCompanyClearer
		p.CreditType = req.CreditType
		p.CCExpDate = req.CCExpDate
		p.CCNumber = req.CCNumber
		p.DebitCode = req.DebitCode
		p.DebitCurrency = req.DebitCurrency
		p.DebitTotal = req.DebitTotal
		p.DebitType = req.DebitType
		p.FirstPaymentTotal = req.FirstPaymentTotal
		p.FixedPaymentTotal = req.FixedPaymentTotal
		p.TotalPayments = req.TotalPayments
		p.JParam = req.JParam
		p.TransactionInitTime = req.TransactionInitTime
		p.TransactionUpdateTime = req.TransactionUpdateTime
		p.VoucherID = req.VoucherID
	} else {
		p.PaymentStatus = null.NewString("failed", true)
		p.ErrorMsg = null.NewString("Failed", true) // TODO: improve
		p.PaymentType = null.NewString("pelecard", true)
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(ctx, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, uint(paymentid)),
			toUpdateArgs...)
		if err != nil {
			return p, fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return p, fmt.Errorf("Payment not Updated")
		}

		toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(p)

		// update payments_pelecard table after payment
		_, pelecardErr := DB.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, uint(paymentid)),
			toUpdateArgsPeleCard...)
		if pelecardErr != nil {
			fmt.Errorf("problem updating payments: %w", err)
		}

	} else {
		fmt.Println("invalid values")
	}

	return p, nil

}

func syncServiceRegistration(ctx *gin.Context, p Payment, o Order) error {
	type RequestSyncRegistration struct {
		FirstName             string `json:"first_name"`
		LastName              string `json:"last_name"`
		Email                 string `json:"email"`
		Event                 string `json:"event"`
		Choice                string `json:"choice"`
		Lang                  string `json:"lang"`
		CommunicationLanguage string `json:"communication_language"`
		TicketStatus          string `json:"ticket_status"`
		KeycloakID            string `json:"keycloakid"`
	}

	var payload RequestSyncRegistration

	var a Account

	if err := DB.QueryRow(ctx, `SELECT 
	"FirstName",
	"LastName",
	"Email",
	"UserKey" 
	FROM accounts WHERE id=$1`, o.AccountID.Int64).Scan(
		&a.FirstName, &a.LastName, &a.Email, &a.UserKey,
	); err != nil {
		return errors.New("cannot find related Order for Payment")
	}

	payload.FirstName = a.FirstName.String
	payload.LastName = a.LastName.String
	payload.Email = a.Email.String
	payload.Event = "jan2022"
	payload.Choice = "ticket"
	payload.Lang = o.OrderLanguage.String
	payload.CommunicationLanguage = o.OrderLanguage.String
	payload.TicketStatus = o.ProductType.String
	payload.KeycloakID = a.UserKey.String

	log.Println(">>> order/synch/payload::")
	log.Println(payload)

	marshaledPayload, _ := json.Marshal(payload)
	url := "http://vh-srv-registration:3200/choice/kc/" + a.UserKey.String
	_, err := postJSON("POST", url, marshaledPayload)

	if err != nil {
		return err
	}

	return nil
}

func updateOrderAfterPayment(ctx *gin.Context, p Payment) (Order, error) {
	var o Order

	if err := DB.QueryRow(ctx, `SELECT 
	id, "ProductType", "AccountID", "OrderLanguage" FROM orders WHERE id=$1`, p.OrderID.Int64).Scan(
		&o.ID, &o.ProductType, &o.AccountID, &o.OrderLanguage,
	); err != nil {
		return o, err
	}

	if p.Success.String == "1" {
		o.Status = null.NewString("paid", true)
		o.PaymentDate = null.NewTime(time.Now(), true)

		updateRes, err := DB.Exec(ctx, `UPDATE orders 
		SET 
		"Status"=$1,
		"PaymentDate"=$2,
		starting_date=$3,
		updated_at=$4 
		WHERE id = $5`, o.Status.String, o.PaymentDate.Time, o.PaymentDate.Time, time.Now(), p.OrderID.Int64)
		if err != nil {
			return o, fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() != 1 {
			return o, fmt.Errorf("orders not Updated")
		}

	} else {
		o.Status = null.NewString("nosuccess", true)
		updateRes, err := DB.Exec(ctx, `UPDATE orders 
		SET 
		"Status"=$1,
		updated_at=$2 
		WHERE id = $3`, o.Status.String, time.Now(), p.OrderID.Int64)
		if err != nil {
			return o, fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() != 1 {
			return o, fmt.Errorf("orders not Updated")
		}
	}

	return o, nil
}

// Renewal function

// Get Order
func getOrderByID(ctx *gin.Context, orderID uint) Order {
	var o Order
	var amount string

	if err := DB.QueryRow(ctx, `SELECT 
	id,
	"Type",
	"ProductType",
	"RecuringFreq",
	"AccountID",
	"Organization",
	"Amount",
	"Currency",
	"Status",
	"OrderLanguage",
	"PaymentDate",
	starting_date,
	"Flag",
	quantity,
	amount_item,
	created_at,
	updated_at,
	deleted_at 
	FROM orders WHERE id=$1`, orderID).Scan(
		&o.ID, &o.Type, &o.ProductType, &o.RecuringFreq, &o.AccountID, &o.Organization, &amount,
		&o.Currency, &o.Status, &o.OrderLanguage, &o.PaymentDate, &o.StartingDate, &o.Flag, &o.Quantity, &o.AmountItem,
		&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
	); err != nil {
		fmt.Println("--get-order-err", err)
		log.Printf("\n## ERROR - NO ORDER %v\n", orderID)
		return o
	}

	value, err := strconv.ParseFloat(amount, 32)

	if err != nil {
		fmt.Println("error converting amount string to float")
		return o
	}

	floatAmount := float64(value)

	o.Amount = null.NewFloat(floatAmount, true)

	return o
}

// Get Payment
func getPaymentForOrderID(ctx *gin.Context, orderID uint) Payment {
	var p Payment
	// result := DB.Where(&Payment{OrderID: orderID, PaymentStatus: "success"}).First(&p)
	// Get payment
	if err := DB.QueryRow(ctx, `SELECT 
	id,
	"Amount",
	"PaymentStatus",
	"PaymentType",
	"OrderID",
	"ParamX",
	"AuthNo",
	confirmation_key,
	success,
	pelecard_token,
	"TransactionID",
	"ErrorMsg",
	"CardHebrewName",
	"CCAbroadCard",
	"CCBrand",
	"CCCompanyClearer",
	"CCCompanyIssuer",
	credit_type,
	"CCExpDate",
	"CCNumber",
	"DebitCode",
	"DebitCurrency",
	"DebitTotal",
	"DebitType",
	"FirstPaymentTotal",
	"FixedPaymentTotal",
	j_param,
	"TotalPayments",
	"TransactionInitTime",
	"TransactionUpdateTime",
	"VoucherID",
	"Ordkey",
	created_at,
	updated_at,
	deleted_at 
	FROM payments WHERE "OrderID"=$1 AND "PaymentStatus"=$2`, orderID, "success").Scan(
		&p.ID, &p.Amount, &p.PaymentStatus, &p.PaymentType, &p.OrderID, &p.ParamX, &p.AuthNo, &p.ConfirmationKey, &p.Success, &p.PelecardToken,
		&p.TransactionID, &p.ErrorMsg, &p.CardHebrewName, &p.CCAbroadCard, &p.CCBrand, &p.CCCompanyClearer,
		&p.CCCompanyIssuer, &p.CreditType, &p.CCExpDate, &p.CCNumber, &p.DebitCode,
		&p.DebitCurrency, &p.DebitTotal, &p.DebitType, &p.FirstPaymentTotal, &p.FixedPaymentTotal, &p.JParam,
		&p.TotalPayments, &p.TransactionInitTime, &p.TransactionUpdateTime, &p.VoucherID,
		&p.Ordkey, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}

	return p
}

// Get Account
func getAccountForOrderID(ctx *gin.Context, orderID uint) Account {
	var a Account
	o := getOrderByID(ctx, orderID)
	// result := DB.Where(&Account{ID: o.AccountID}).First(&a)

	if err := DB.QueryRow(ctx, `SELECT 
	id,
	"FirstName",
	"LastName",
	"Email",
	"Phone",
	"Street",
	"City",
	"State",
	"Postcode",
	"Country",
	"AccountType",
	"PaymentToken",
	"PaymentCardID",
	"PaymentCardExpMonth",
	"PaymentCardExpYear",
	"UserKey",
	"AuthNo", 
	created_at,
	updated_at,
	deleted_at 
	FROM accounts WHERE id=$1`, o.AccountID.Int64).Scan(
		&a.ID, &a.FirstName, &a.LastName, &a.Email, &a.Phone, &a.Street, &a.City, &a.State, &a.Postcode, &a.Country,
		&a.AccountType, &a.PaymentToken, &a.PaymentCardID, &a.PaymentCardExpMonth, &a.PaymentCardExpYear, &a.UserKey,
		&a.AuthNo, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}

	return a
}

// TODO: REFACTOR
func createRequestPayByToken(c *gin.Context, a Account, o Order, p Payment, pmx null.String) (RequestPayment, Payment) {
	newp, _ := createPendingPayment(c, o.Amount, o.ID, pmx)
	newp.PelecardToken = p.PelecardToken
	newp.AuthNo = p.AuthNo

	userFullName := a.FirstName.String + " " + a.LastName.String

	extPay := RequestPayment{
		UserKey: newp.Ordkey.String,

		GoodURL:    "http://ec41a043fda1.ngrok.io/pelecard/good",
		ErrorURL:   "http://ec41a043fda1.ngrok.io/pelecard/error",
		CancelURL:  "http://ec41a043fda1.ngrok.io/pelecard/cancel",
		ApprovalNo: p.AuthNo.String,
		Token:      p.PelecardToken.String,

		Name:         userFullName,
		Price:        o.Amount.Float64,
		Currency:     o.Currency.String,
		Email:        a.Email.String,
		Phone:        "+NA",
		Street:       a.Street.String,
		City:         a.City.String,
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          "40037",
		VAT:          "f",
		Installments: 1,
		Language:     o.OrderLanguage.String,
		Reference:    newp.ParamX.String,
		Organization: "ben2",
	}

	return extPay, newp
}

func renewPaymentByToken(extPay RequestPayment, pmx string) (interface{}, error) {
	payload, _ := json.Marshal(extPay)
	var url string
	if pmx == "t" {
		url = "https://checkout.kbb1.com/token/charge"
	} else if pmx == "e" {
		url = "https://checkout.kbb1.com/emv/charge"
	}
	resp, err := postJSON("POST", url, payload)
	defer resp.Body.Close()
	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	parsableBody := string(body)
	//actualURL := strings.Split(parsableBody, "'")[1]

	fmt.Println("response URL:", parsableBody)
	var i interface{}
	json.Unmarshal(body, &i)
	fmt.Println(i)
	return i, err
}

func renewOrder(c *gin.Context, orderID uint, pmx string) string {
	/*
			get account by order
			if no token in account
			get payment for order
			extract token
			make payment by token
			if payment successfull (handled in /pelecard good then ... )
			TODO update account with token
			TODO update order

		var a Account

	*/
	o := getOrderByID(c, orderID)
	p := getPaymentForOrderID(c, orderID)
	a := getAccountForOrderID(c, orderID)

	// if a.PaymentToken == "" {
	// 	fmt.Printf("##\nTOKEN IS NULL \n##\n")
	// 	a.PaymentToken = p.PelecardToken
	// 	// add other parameter
	// 	// parse payment card stuff (split and convert to int)
	// 	DB.Model(&a).Uoken(a, o, p, pmx)
	pr, newp := createRequestPayByToken(c, a, o, p, null.NewString(pmx, true))
	resp, err := renewPaymentByToken(pr, pmx)
	if err != nil {
		newp.PaymentStatus = null.NewString("failed", true)
		newp.Success = null.NewString("0", true)
	}
	answers := resp.(map[string]interface{})
	if answers["status"].(string) == "success" {
		successTxt := "success"
		oneTxt := "1"
		newp.PaymentStatus = null.NewString(successTxt, true)
		newp.Success = null.NewString(oneTxt, true)
		data := answers["data"].(string)
		fmt.Println(data)
		flagOrderAsRenewed(c, orderID)
	} else {
		newp.PaymentStatus = null.NewString("failed", true)
		newp.Success = null.NewString("0", true)
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(newp)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, newp.ID),
			toUpdateArgs...)
		if err != nil {
			fmt.Errorf("problem updating payments: %w", err)
		}

		toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(newp)

		// update payments_pelecard table after payment
		_, pelecardErr := DB.Exec(c, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, newp.ID),
			toUpdateArgsPeleCard...)
		if pelecardErr != nil {
			fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return newp.Success.String
		}

	} else {
		fmt.Println("invalid values")
	}
	updateOrderAfterPayment(c, newp)
	return newp.Success.String
}

func flagOrderAsRenewed(ctx *gin.Context, orderID uint) {
	// req := `update orders
	// 	set "Flag" = 'renewed'
	// 	where id = ?`

	// res := DB.Exec(req, orderID)

	updateRes, err := DB.Exec(ctx, `UPDATE orders 
		SET 
		"Flag"=$1,
		updated_at=$2 
		WHERE id = $3`,
		"renewed", time.Now(), orderID)
	if err != nil {
		fmt.Println("problem updating orders: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println("no rows affected")
	}
}

func chargeOrdersToRenew(c *gin.Context, pmx string) int {
	sqlQuery := `
	Select id from orders 
	Where ("Status" = 'paid' or "Status" = 'nosuccess')
	and "Type" = 'recurring'
	and "Flag" = 'torenew'
	`
	rows, err := DB.Query(c, sqlQuery)

	if err != nil {
		return -1
	}
	defer rows.Close()

	var count int
	var id int
	count = 0

	for rows.Next() {
		err := rows.Scan(&id)
		if err != nil {
			return -1
		}
		fmt.Printf(">>> Renewing %d\n", id)
		status := renewOrder(c, uint(id), pmx)
		//status := "1"
		if status == "1" {
			count++
		} else {
			log.Printf("## Error with %v", id)
		}
	}
	return count
}

func flagDuplicateOrders(ctx *gin.Context, ProductType string) int {
	req := `select "AccountID" as id, count(*) as "duplicate" 
from orders where "Status" = 'paid' 
group by "AccountID" 
having count(*) > 1
order by duplicate desc`

	// rows, err := DB.Raw(req).Rows() // (*sql.Rows, error)
	rows, err := DB.Query(ctx, req)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	var b int
	for rows.Next() {
		err := rows.Scan(&id, &b)
		if err != nil {
			return -1
		}
		flagOrdersByAccountID(ctx, id, "duplicate")
		count++
	}
	return count
}

func createV2Order(ctx *gin.Context, o Order) (int, error) {

	createString, numString, createQueryArgs := prepareOrderCreateQuery(o)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := DB.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
			createQueryArgs...).Scan(
			&ID,
		); err != nil {
			return 0, err
		}
		return ID, nil
	} else {
		return 0, errInvalidBody
	}

}

func softDeleteOrderByID(c *gin.Context, orderID int) error {
	_, err := DB.Exec(c, "UPDATE orders SET deleted_at = $1 WHERE id = $2", time.Now(), orderID)
	return err
}

func patchOrderByID(c *gin.Context, req Order, orderId int) error {

	toUpdate, toUpdateArgs := prepareOrderUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, orderId),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating order: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("order not updated as no rows affected")
		}

	} else {
		return errInvalidBody
	}

	return nil
}

func addFlagToOrder(ctx *gin.Context, oid uint, flag string) {
	o := getOrderByID(ctx, uint(oid))
	o.Flag = null.NewString(flag, true)

	if o.ID == 0 {
		fmt.Println("order not found")
		return
	}

	toUpdate, toUpdateArgs := prepareOrderUpdateQuery(o)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(ctx, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, o.ID),
			toUpdateArgs...)
		if err != nil {
			fmt.Println("problem updating orders: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			fmt.Println("no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}
}

func flagOrdersByAccountID(ctx *gin.Context, aid int, flag string) int {
	req := `select id from orders where "AccountID" = $1 and "Status" = 'paid'`
	rows, err := DB.Query(ctx, req, aid)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	for rows.Next() {
		rows.Scan(&id)
		addFlagToOrder(ctx, uint(id), flag)
		count++
	}
	return count

}

func GetAllOrders(ctx *gin.Context, skip int, limit int, fromDate string, toDate *time.Time, productType string, currency string, status string, organisation string, email string, accountID int, evaluateMembership string, orderByPaymentDate string) (*[]Order, error) {

	orders := []Order{}

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)

	whereQuery, orderByQuery, queryBuildErr := buildAndGetOrdersWhereQuery(fromDate, toDate, productType, currency, status, organisation, email, accountID, evaluateMembership, orderByPaymentDate)

	if queryBuildErr != nil {
		return &orders, queryBuildErr
	}

	fromQuery := " FROM orders as o"

	if email != "" {
		fromQuery = fromQuery + ", accounts as a"
	}

	rows, err := DB.Query(ctx, `SELECT 
		o.id, o."Type", o."ProductType", o."RecuringFreq", o."AccountID", o."Organization", o."Amount", 
		"Currency", o."Status", o."OrderLanguage", o."PaymentDate", o.starting_date, o."SKU", o."Note", o."Flag", o.quantity, o.amount_item,
		 o.created_at, o.updated_at, o.deleted_at
	`+fromQuery+whereQuery+orderByQuery+limitOffsetString)

	if err != nil {
		fmt.Println("--error-while-executing-query", err)
		return &orders, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Order
		err := rows.Scan(
			&d.ID, &d.Type, &d.ProductType, &d.RecuringFreq, &d.AccountID, &d.Organization, &d.Amount,
			&d.Currency, &d.Status, &d.OrderLanguage, &d.PaymentDate, &d.StartingDate, &d.SKU, &d.Note, &d.Flag, &d.Quantity, &d.AmountItem,
			&d.CreatedAt, &d.UpdatedAt, &d.DeletedAt)
		if err != nil {
			return &orders, err
		}
		orders = append(orders, d)
	}
	return &orders, rows.Err()

}

func prepareOrderCreateQuery(req Order) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.Type.Valid {
		createStrings = append(createStrings, `"Type"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Type.String)
	}
	if req.ProductType.Valid {
		createStrings = append(createStrings, `"ProductType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ProductType.String)
	}
	if req.RecuringFreq.Valid {
		createStrings = append(createStrings, `"RecuringFreq"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.RecuringFreq.Int64)
	}
	if req.AccountID.Valid {
		createStrings = append(createStrings, `"AccountID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AccountID.Int64)
	}
	if req.Organization.Valid {
		createStrings = append(createStrings, `"Organization"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Organization.String)
	}
	if req.Amount.Valid {
		createStrings = append(createStrings, `"Amount"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, fmt.Sprintf("%g", req.Amount.Float64))
	}
	if req.Currency.Valid {
		createStrings = append(createStrings, `"Currency"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Currency.String)
	}
	if req.SKU.Valid {
		createStrings = append(createStrings, `"SKU"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.SKU.String)
	}
	if req.Status.Valid {
		createStrings = append(createStrings, `"Status"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Status.String)
	}
	if req.OrderLanguage.Valid {
		createStrings = append(createStrings, `"OrderLanguage"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.OrderLanguage.String)
	}
	if req.PaymentDate.Valid {
		createStrings = append(createStrings, `"PaymentDate"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentDate.Time)
	}
	if req.Note.Valid {
		createStrings = append(createStrings, `"Note"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Note.String)
	}
	if req.Flag.Valid {
		createStrings = append(createStrings, `"Flag"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Flag.String)
	}
	if req.Quantity.Valid {
		createStrings = append(createStrings, "quantity")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Quantity.Int64)
	}
	if req.AmountItem.Valid {
		createStrings = append(createStrings, "amount_item")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AmountItem.Int64)
	}
	if req.StartingDate.Valid {
		createStrings = append(createStrings, "starting_date")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.StartingDate.Time)
	}

	if len(args) != 0 {
		createStrings = append(createStrings, "created_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())

		createStrings = append(createStrings, "updated_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}

func prepareOrderUpdateQuery(req Order) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.Type.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Type"=$%d`, len(updateStrings)+1))
		args = append(args, req.Type.String)
	}
	if req.ProductType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"ProductType"=$%d`, len(updateStrings)+1))
		args = append(args, req.ProductType.String)
	}
	if req.RecuringFreq.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"RecuringFreq"=$%d`, len(updateStrings)+1))
		args = append(args, req.RecuringFreq.Int64)
	}
	if req.AccountID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"AccountID"=$%d`, len(updateStrings)+1))
		args = append(args, req.AccountID.Int64)
	}
	if req.Organization.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Organization"=$%d`, len(updateStrings)+1))
		args = append(args, req.Organization.String)
	}
	if req.Amount.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Amount"=$%d`, len(updateStrings)+1))
		args = append(args, fmt.Sprintf("%g", req.Amount.Float64))
	}
	if req.Currency.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Currency"=$%d`, len(updateStrings)+1))
		args = append(args, req.Currency.String)
	}
	if req.SKU.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"SKU"=$%d`, len(updateStrings)+1))
		args = append(args, req.SKU.String)
	}
	if req.Status.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Status"=$%d`, len(updateStrings)+1))
		args = append(args, req.Status.String)
	}
	if req.OrderLanguage.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"OrderLanguage"=$%d`, len(updateStrings)+1))
		args = append(args, req.OrderLanguage.String)
	}
	if req.PaymentDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"PaymentDate"=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentDate.Time)
	}
	if req.StartingDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`starting_date=$%d`, len(updateStrings)+1))
		args = append(args, req.StartingDate.Time)
	}
	if req.Note.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Note"=$%d`, len(updateStrings)+1))
		args = append(args, req.Note.String)
	}
	if req.Flag.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Flag"=$%d`, len(updateStrings)+1))
		args = append(args, req.Flag.String)
	}
	if req.Quantity.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`quantity=$%d`, len(updateStrings)+1))
		args = append(args, req.Quantity.Int64)
	}
	if req.AmountItem.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`amount_item=$%d`, len(updateStrings)+1))
		args = append(args, req.AmountItem.Int64)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func buildAndGetOrdersWhereQuery(fromDate string, dateTo *time.Time, productType string, currency string, status string, organisation string, email string, accountID int, evaluateMembership string, orderByPaymentDate string) (string, string, error) {

	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	// time format with timezone
	whereCondition.WriteString(fmt.Sprintf(" o.updated_at <= '%s'", dateTo.Format(time.RFC3339Nano)))

	// WHERE query generation based on parameters
	if fromDate != "" {
		rfcLayout := time.RFC3339
		fromDateParsed, err := time.Parse(rfcLayout, fromDate)

		if err != nil {
			return "", "", err
		}
		whereCondition.WriteString(fmt.Sprintf(" AND o.updated_at >= '%s'", fromDateParsed.Format("2006-01-02 15:04:05")))
	}

	if currency != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(o.\"Currency\")=LOWER('%s')", currency))
	}

	if status != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(o.\"Status\")=LOWER('%s')", status))
	}

	if productType != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(o.\"ProductType\")=LOWER('%s')", productType))
	}

	if organisation != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(o.\"Organization\")=LOWER('%s')", organisation))
	}

	if accountID != 0 {
		whereCondition.WriteString(fmt.Sprintf(" AND o.\"AccountID\" = %d", accountID))
	}

	if email != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND o.\"AccountID\" = a.id AND LOWER(a.\"Email\")=LOWER('%s')", email))
	}

	if evaluateMembership != "" {
		if evaluateMembership == "true" {
			whereCondition.WriteString(" AND (o.\"Status\" = 'paid' OR o.\"Status\" = 'success' OR o.\"Status\" = 'nosuccess' OR o.\"Status\" = 'cancelled')")
		}
	}

	if orderByPaymentDate != "" {
		if strings.ToLower(orderByPaymentDate) != "desc" && strings.ToLower(orderByPaymentDate) != "asc" {
			orderByPaymentDate = "asc"
		}
		orderBy.WriteString(fmt.Sprintf(" ORDER BY o.\"PaymentDate\" %s", orderByPaymentDate))
	} else {
		orderBy.WriteString(fmt.Sprintf(" ORDER BY updated_at %s", "desc"))
	}

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}
