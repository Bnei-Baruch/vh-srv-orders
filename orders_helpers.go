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

func createOrder(c *gin.Context, req RequestOrder) (Order, error) {
	o := Order{
		Type:          req.Type,
		ProductType:   req.ProductType,
		RecuringFreq:  req.RecurringFreq,
		Organization:  req.Organization,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        "pending",
		OrderLanguage: req.OrderLanguage,
		AccountID:     0,
	}

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

		AccountType: "personal",
		UserKey:     req.UserKey,
	}

	accountID := CreateOrUpdateAccount(c, a)

	if accountID == 0 {
		return o, errors.New("Null account")
	}

	o.AccountID = accountID

	execRes, err := DB.Exec(c, `INSERT INTO orders (
		"Type",
		"ProductType",
		"RecuringFreq",
		"Organization",
		"Amount",
		"Currency",
		"Status",
		"OrderLanguage",
		"AccountID",
		created_at,
		updated_at
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		o.Type, o.ProductType, o.RecuringFreq, o.Organization, fmt.Sprintf("%g", o.Amount), o.Currency, o.Status, o.OrderLanguage, o.AccountID, time.Now(), time.Now())

	if err != nil {
		return o, err
	}

	if execRes.RowsAffected() == 0 {
		return o, fmt.Errorf("No rows affected")
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

	p := Payment{
		Amount:        req.Amount,
		PaymentType:   "pelecard",
		OrderID:       o.ID,
		PaymentStatus: "pending",
	}

	execRes, err := DB.Exec(c, `INSERT INTO orders (
		Amount,
		PaymentType,
		OrderID,
		PaymentStatus
	)
	VALUES (
		$1, $2, $3, $4)`,
		p.Amount, p.PaymentType, p.OrderID, p.PaymentStatus)

	if err != nil {
		return p, err
	}

	if execRes.RowsAffected() == 0 {
		return p, fmt.Errorf("No rows affected")
	}

	return p, nil

}

func createPendingPayment(c *gin.Context, sum float32, oid uint, pmx string) (Payment, error) {

	p := Payment{
		Amount:        sum,
		PaymentType:   "pelecard",
		OrderID:       oid,
		PaymentStatus: "pending",
	}

	var ID int64
	// Add new account if not exist
	if err := DB.QueryRow(c, `INSERT INTO payments (
		Amount,
		PaymentType,
		OrderID,
		PaymentStatus
	)
	VALUES (
		$1, $2, $3, $4) RETURNING id`,
		p.Amount, p.PaymentType, p.OrderID, p.PaymentStatus).Scan(
		&ID,
	); err != nil {
		return p, err
	}

	paramx := "mb-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX") + pmx
	ordkey := "ord-" + strconv.FormatUint(uint64(oid), 10) + os.Getenv("SUFX")
	fmt.Printf(">>>> ParamX: %s\n", paramx)

	p.ParamX = paramx
	p.Ordkey = ordkey

	updateRes, err := DB.Exec(c, `UPDATE payments 
		SET
		ParamX=$1,
		Ordkey=$2,
		updated_at=$3 
		WHERE id = $4`,
		p.ParamX, p.Ordkey, time.Now(), p.ID)
	if err != nil {
		return p, err
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println(updateRes.RowsAffected())
		return p, fmt.Errorf("No rows affected")
	}

	return p, nil
}

func updatePayment(ctx *gin.Context, req RequestPaid) (Payment, error) {
	var p Payment

	if len(req.Error) > 0 {
		return p, errors.New(req.Error)
	}

	orderid, err := strconv.ParseUint(strings.Split(req.UserKey, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}
	paymentid, err := strconv.ParseUint(strings.Split(req.ParamX, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}

	// Get payment
	if err := DB.QueryRow(ctx, `SELECT 
	PaymentStatus,
	PaymentType,
	ParamX,
	AuthNo,
	confirmation_key,
	success,
	pelecard_token,
	TransactionID,
	CCBrand,
	CardHebrewName,
	CCAbroadCard,
	CCCompanyClearer,
	credit_type,
	CCExpDate,
	CCNumber,
	DebitCode,
	DebitCurrency,
	DebitTotal,
	DebitType,
	FirstPaymentTotal,
	FixedPaymentTotal,
	TotalPayments,
	j_param,
	TransactionInitTime,
	TransactionUpdateTime,
	VoucherID FROM payments WHERE OrderID=$1 AND id=$2`, uint(orderid), uint(paymentid)).Scan(
		&p,
	); err != nil {
		if err == pgx.ErrNoRows {
			return p, errors.New("Cannot find related Order for Payment")
		}
	}

	// if DB.Where(&Payment{OrderID: uint(orderid), ID: uint(paymentid)}).First(&p).RecordNotFound() {
	// 	return p, errors.New("Cannot find related Order for Payment")
	// }

	//update payment object
	if req.Success == "1" {
		p.PaymentStatus = "success"
		p.PaymentType = "pelecard"
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
		p.PaymentStatus = "failed"
		p.ErrorMsg = "Failed" // TODO: improve
		p.PaymentType = "pelecard"
	}

	// DB.Model(&p).Updates(p)

	updateRes, err := DB.Exec(ctx, `UPDATE payments 
		SET
		PaymentStatus=$1,
		PaymentType=$2,
		ParamX=$3,
		AuthNo=$4,
		confirmation_key=$5,
		success=$6,
		pelecard_token=$7,
		TransactionID=$8,
		CCBrand=$9,
		CardHebrewName=$10,
		CCAbroadCard=$11,
		CCCompanyClearer=$12,
		credit_type=$13,
		CCExpDate=$14,
		CCNumber=$15,
		DebitCode=$16,
		DebitCurrency=$17,
		DebitTotal=$18,
		DebitType=$19,
		FirstPaymentTotal=$20,
		FixedPaymentTotal=$21,
		TotalPayments=$22,
		j_param=$23,
		TransactionInitTime=$24,
		TransactionUpdateTime=$25,
		VoucherID=$26,
		ErrorMsg=$27,
		updated_at=$28 
		WHERE id = $29`,
		p.PaymentStatus, p.PaymentType, p.ParamX, p.AuthNo, p.ConfirmationKey, p.Success, p.PelecardToken,
		p.TransactionID, p.CCBrand, p.CardHebrewName, p.CCAbroadCard, p.CCCompanyClearer, p.CreditType, p.CCExpDate,
		p.CCNumber, p.DebitCode, p.DebitCurrency, p.DebitTotal, p.DebitType, p.FirstPaymentTotal, p.FixedPaymentTotal,
		p.TotalPayments, p.JParam, p.TransactionInitTime, p.TransactionUpdateTime, p.VoucherID, p.ErrorMsg, time.Now(), p.ID)
	if err != nil {
		fmt.Errorf("problem updating payments: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		return p, fmt.Errorf("Payment not Updated")
	}

	if err != nil {
		return p, err
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
	FirstName,
	LastName,
	Email,
	UserKey 
	FROM orders WHERE id=$1`, o.AccountID).Scan(
		&a,
	); err != nil {
		return errors.New("Cannot find related Order for Payment")
	}

	payload.FirstName = a.FirstName
	payload.LastName = a.LastName
	payload.Email = a.Email
	payload.Event = "jan2022"
	payload.Choice = "ticket"
	payload.Lang = o.OrderLanguage
	payload.CommunicationLanguage = strings.ToLower(o.OrderLanguage)
	payload.TicketStatus = o.ProductType
	payload.KeycloakID = a.UserKey

	log.Println(">>> order/synch/payload::")
	log.Println(payload)

	marshaledPayload, _ := json.Marshal(payload)
	url := "http://vh-srv-registration:3200/choice/kc/" + a.UserKey
	_, err := postJSON("POST", url, marshaledPayload)

	if err != nil {
		return err
	}

	return nil
}

func updateOrderAfterPayment(ctx *gin.Context, p Payment) (Order, error) {
	var o Order

	if err := DB.QueryRow(ctx, `SELECT 
	Status,
	PaymentDate,
	VoucherID FROM orders WHERE id=$1`, p.OrderID).Scan(
		&o,
	); err != nil {
		return o, err
	}

	if p.Success == "1" {
		o.Status = "paid"
		o.PaymentDate = time.Now()
	} else {
		o.Status = "nosuccess"
	}

	updateRes, err := DB.Exec(ctx, `UPDATE payments 
		SET
		Status=$1,
		PaymentDate=$2,
		updated_at=$3 
		WHERE id = $4`,
		o.Status, o.PaymentDate, time.Now(), p.OrderID)
	if err != nil {
		fmt.Errorf("problem updating payments: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		return o, fmt.Errorf("Payment not Updated")
	}

	if err != nil {
		return o, err
	}

	return o, nil
}

// Renewal function

//Get Order
func getOrderByID(ctx *gin.Context, orderID uint) Order {
	var o Order

	if err := DB.QueryRow(ctx, `SELECT 
	id,
	Type,
	ProductType,
	RecuringFreq,
	AccountID,
	Organization,
	Amount,
	Currency,
	Status,
	OrderLanguage,
	PaymentDate,
	SKU,
	Note,
	Flag,
	created_at,
	updated_at,
	deleted_at,
	FROM orders WHERE id=$1`, orderID).Scan(
		&o.ID, &o.Type, &o.ProductType, &o.RecuringFreq, &o.AccountID, &o.Organization, &o.Amount,
		&o.Currency, &o.Status, &o.OrderLanguage, &o.PaymentDate, &o.SKU, &o.Note, &o.Flag, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO ORDER %v\n", orderID)
		return o
	}
	// result := DB.Where(&Order{ID: orderID}).First(&o)

	return o
}

//Get Payment
func getPaymentForOrderID(ctx *gin.Context, orderID uint) Payment {
	var p Payment
	// result := DB.Where(&Payment{OrderID: orderID, PaymentStatus: "success"}).First(&p)
	// Get payment
	if err := DB.QueryRow(ctx, `SELECT 
	id,
	Amount,
	PaymentStatus,
	PaymentType,
	OrderID,
	ParamX,
	AuthNo,
	confirmation_key,
	success,
	pelecard_token,
	TransactionID,
	ErrorMsg,
	CardHebrewName,
	CCAbroadCard,
	CCBrand,
	CCCompanyClearer,
	CCCompanyIssuer,
	credit_type,
	CCExpDate,
	CCNumber,
	DebitCode,
	DebitCurrency,
	DebitTotal,
	DebitType,
	FirstPaymentTotal,
	FixedPaymentTotal,
	j_param,
	TotalPayments,
	TransactionInitTime,
	TransactionUpdateTime,
	VoucherID,
	Ordkey,
	created_at,
	updated_at,
	deleted_at 
	FROM payments WHERE OrderID=$1 AND PaymentStatus=$2`, orderID, "success").Scan(
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
	FirstName,
	LastName,
	Email,
	Phone,
	Street,
	City,
	State,
	Postcode,
	Country,
	AccountType,
	PaymentToken,
	PaymentCardID,
	PaymentCardExpMonth,
	PaymentCardExpYear,
	UserKey,
	AuthNo, 
	created_at,
	updated_at,
	deleted_at 
	FROM accounts WHERE id=$1`, o.AccountID).Scan(
		&a.ID, &a.FirstName, &a.LastName, &a.Email, &a.Phone, &a.Street, &a.City, &a.State, &a.Postcode, &a.Country,
		&a.AccountType, &a.PaymentToken, &a.PaymentCardID, &a.PaymentCardExpMonth, &a.PaymentCardExpYear, &a.UserKey,
		&a.AuthNo, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}

	return a
}

// TODO: REFACTOR
func createRequestPayByToken(c *gin.Context, a Account, o Order, p Payment, pmx string) (RequestPayment, Payment) {
	newp, _ := createPendingPayment(c, o.Amount, o.ID, pmx)
	newp.PelecardToken = p.PelecardToken
	newp.AuthNo = p.AuthNo

	extPay := RequestPayment{
		UserKey: newp.Ordkey,

		GoodURL:    "http://ec41a043fda1.ngrok.io/pelecard/good",
		ErrorURL:   "http://ec41a043fda1.ngrok.io/pelecard/error",
		CancelURL:  "http://ec41a043fda1.ngrok.io/pelecard/cancel",
		ApprovalNo: p.AuthNo,
		Token:      p.PelecardToken,

		Name:         a.FirstName + " " + a.LastName,
		Price:        o.Amount,
		Currency:     o.Currency,
		Email:        a.Email,
		Phone:        "+NA",
		Street:       a.Street,
		City:         a.City,
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          "40037",
		VAT:          "f",
		Installments: 1,
		Language:     o.OrderLanguage,
		Reference:    newp.ParamX,
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
	pr, newp := createRequestPayByToken(c, a, o, p, pmx)
	resp, err := renewPaymentByToken(pr, pmx)
	if err != nil {
		newp.PaymentStatus = "failed"
		newp.Success = "0"
	}
	answers := resp.(map[string]interface{})
	// data := answers["data"].(string)
	// fmt.Println(data)
	if answers["status"].(string) == "success" {
		newp.PaymentStatus = "success"
		newp.Success = "1"
		data := answers["data"].(string)
		fmt.Println(data)
		flagOrderAsRenewed(c, orderID)
	} else {
		newp.PaymentStatus = "failed"
		newp.Success = "0"
	}
	// DB.Model(&newp).Updates(newp)

	updateRes, err := DB.Exec(c, `UPDATE payments 
		SET
		Amount=$1,
		PaymentStatus=$2,
		PaymentType=$3,
		OrderID=$4,
		ParamX=$5,
		AuthNo=$6,
		success=$7,
		pelecard_token=$8,
		Ordkey=$9,
		updated_at=$10 
		WHERE id = $11`,
		newp.Amount, newp.PaymentStatus, newp.PaymentType, newp.OrderID, newp.ParamX, newp.AuthNo,
		newp.Success, newp.PelecardToken, newp.Ordkey, time.Now(), newp.ID)
	if err != nil {
		fmt.Errorf("problem updating payments: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println(updateRes.RowsAffected())
		// c.JSON(http.StatusNotFound, gin.H{"error": "Payment not Updted"})
		return newp.Success
	}
	updateOrderAfterPayment(c, newp)
	return newp.Success
}

func flagOrderAsRenewed(ctx *gin.Context, orderID uint) {
	// req := `update orders
	// 	set "Flag" = 'renewed'
	// 	where id = ?`

	// res := DB.Exec(req, orderID)

	updateRes, err := DB.Exec(ctx, `UPDATE orders 
		SET 
		Flag=$1,
		updated_at=$2,
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
		// rows.Scan(&id)
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
func addFlagToOrder(ctx *gin.Context, oid uint, flag string) {
	o := getOrderByID(ctx, uint(oid))
	o.Flag = flag

	if o.ID != 0 {
		fmt.Println("order not found")
		return
	}

	updateRes, err := DB.Exec(ctx, `UPDATE orders 
		SET 
		Flag=$1,
		updated_at=$2,
		WHERE id=$3`, flag, time.Now(), o.ID)
	if err != nil {
		fmt.Println("problem updating orders: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println("no rows affected")
	}

	// DB.Model(&o).Updates(o)
}

func flagOrdersByAccountID(ctx *gin.Context, aid int, flag string) int {
	req := `select id from orders where "AccountID" = $1 and "Status" = 'paid'`
	// rows, err := DB.Raw(req, aid).Rows() // (*sql.Rows, error)
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
