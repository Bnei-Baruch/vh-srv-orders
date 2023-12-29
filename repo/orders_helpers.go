package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

func (o *OrdersDB) UpdateOrderStatusByOrderID(ctx context.Context, oid int, status string) error {
	if _, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1 WHERE id=$2`, status, oid); err != nil {
		return err
	}
	return nil
}

func (o *OrdersDB) CreateOrderViaTransaction(ctx context.Context, req RequestOrder) (Order, error) {

	order_status := "pending"
	account_id := 0

	order := Order{
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

	accountID := o.CreateOrUpdateAccount(ctx, a)

	if accountID == 0 {
		return order, errors.New("null account")
	}

	order.AccountID = null.NewInt(int(accountID), true)

	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)

	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&order.ID,
	); err != nil {
		if err == pgx.ErrNoRows {
			return order, fmt.Errorf("no rows affected")
		}
		return order, err
	}

	o.emitEvent(ctx, events.TypeCreateOrder, map[string]interface{}{"order_id": order.ID})

	return order, nil
}

func (o *OrdersDB) SyncServiceRegistration(ctx context.Context, p Payment, order Order) error {
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

	if err := o.QueryRow(ctx, `SELECT 
	"FirstName",
	"LastName",
	"Email",
	"UserKey" 
	FROM accounts WHERE id=$1`, order.AccountID.Int).Scan(
		&a.FirstName, &a.LastName, &a.Email, &a.UserKey,
	); err != nil {
		return errors.New("cannot find related Order for Payment")
	}

	payload.FirstName = a.FirstName.String
	payload.LastName = a.LastName.String
	payload.Email = a.Email.String
	payload.Event = "jan2022"
	payload.Choice = "ticket"
	payload.Lang = order.OrderLanguage.String
	payload.CommunicationLanguage = order.OrderLanguage.String
	payload.TicketStatus = order.ProductType.String
	payload.KeycloakID = a.UserKey.String

	log.Println(">>> order/synch/payload::")
	log.Println(payload)

	marshaledPayload, _ := json.Marshal(payload)
	url := "http://vh-srv-registration:3200/choice/kc/" + a.UserKey.String
	_, err := utils.PostJSON("POST", url, marshaledPayload)

	if err != nil {
		return err
	}

	return nil
}

func (o *OrdersDB) UpdateOrderAfterPayment(ctx context.Context, p Payment) (Order, error) {
	var order Order

	if err := o.QueryRow(ctx, `SELECT 
	id, "ProductType", "AccountID", "OrderLanguage" FROM orders WHERE id=$1`, p.OrderID.Int).Scan(
		&order.ID, &order.ProductType, &order.AccountID, &order.OrderLanguage,
	); err != nil {
		return order, err
	}

	if p.Success.String == "1" {
		order.Status = null.NewString("paid", true)
		order.PaymentDate = null.NewTime(time.Now(), true)

		updateRes, err := o.Exec(ctx, `UPDATE orders 
		SET 
		"Status"=$1,
		"PaymentDate"=$2,
		updated_at=$3 
		WHERE id = $4`, order.Status.String, order.PaymentDate.Time, time.Now(), p.OrderID.Int)
		if err != nil {
			return order, fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() != 1 {
			return order, fmt.Errorf("orders not Updated")
		}

	} else {
		order.Status = null.NewString("nosuccess", true)
		updateRes, err := o.Exec(ctx, `UPDATE orders 
		SET 
		"Status"=$1,
		updated_at=$2 
		WHERE id = $3`, order.Status.String, time.Now(), p.OrderID.Int)
		if err != nil {
			return order, fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() != 1 {
			return order, fmt.Errorf("orders not Updated")
		}
	}

	o.emitEvent(ctx, events.TypeUpdateOrder, map[string]interface{}{"order_id": order.ID})

	return order, nil
}

// Renewal function

// Get Order
func (o *OrdersDB) GetOrderByID(ctx context.Context, orderID uint) Order {
	var order Order
	var amount string

	if err := o.QueryRow(ctx, `SELECT 
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
		&order.ID, &order.Type, &order.ProductType, &order.RecuringFreq, &order.AccountID, &order.Organization, &amount,
		&order.Currency, &order.Status, &order.OrderLanguage, &order.PaymentDate, &order.StartingDate, &order.Flag, &order.Quantity, &order.AmountItem,
		&order.CreatedAt, &order.UpdatedAt, &order.DeletedAt,
	); err != nil {
		fmt.Println("--get-order-err", err)
		log.Printf("\n## ERROR - NO ORDER %v\n", orderID)
		return order
	}

	value, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		fmt.Println("error converting amount string to float")
		return order
	}
	order.Amount = null.NewFloat64(value, true)

	return order
}

// Get Payment
func (o *OrdersDB) GetPaymentForOrderID(ctx context.Context, orderID uint) Payment {
	var p Payment
	if err := o.QueryRow(ctx, `SELECT 
	id,
	"Amount",
	"Currency",
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
		&p.ID, &p.Amount, &p.Currency, &p.PaymentStatus, &p.PaymentType, &p.OrderID, &p.ParamX, &p.AuthNo,
		&p.ConfirmationKey, &p.Success, &p.PelecardToken, &p.TransactionID, &p.ErrorMsg, &p.CardHebrewName,
		&p.CCAbroadCard, &p.CCBrand, &p.CCCompanyClearer, &p.CCCompanyIssuer, &p.CreditType, &p.CCExpDate, &p.CCNumber,
		&p.DebitCode, &p.DebitCurrency, &p.DebitTotal, &p.DebitType, &p.FirstPaymentTotal, &p.FixedPaymentTotal,
		&p.JParam, &p.TotalPayments, &p.TransactionInitTime, &p.TransactionUpdateTime, &p.VoucherID, &p.Ordkey,
		&p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}

	return p
}

// Get Account
func (o *OrdersDB) GetAccountForOrderID(ctx context.Context, orderID uint) Account {
	var a Account
	order := o.GetOrderByID(ctx, orderID)
	// result := DB.Where(&Account{ID: o.AccountID}).First(&a)

	if err := o.QueryRow(ctx, `SELECT 
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
	FROM accounts WHERE id=$1`, order.AccountID.Int).Scan(
		&a.ID, &a.FirstName, &a.LastName, &a.Email, &a.Phone, &a.Street, &a.City, &a.State, &a.Postcode, &a.Country,
		&a.AccountType, &a.PaymentToken, &a.PaymentCardID, &a.PaymentCardExpMonth, &a.PaymentCardExpYear, &a.UserKey,
		&a.AuthNo, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	); err != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}

	return a
}

// TODO: REFACTOR
func (o *OrdersDB) createRequestPayByToken(c context.Context, a Account, order Order, p Payment, pmx null.String) (RequestPayment, Payment) {
	newp, _ := o.createPendingPayment(c, order, pmx)
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
		Price:        order.Amount.Float64,
		Currency:     order.Currency.String,
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
		Language:     order.OrderLanguage.String,
		Reference:    newp.ParamX.String,
		Organization: "ben2",
	}

	return extPay, newp
}

func (o *OrdersDB) renewPaymentByToken(extPay RequestPayment, pmx string) (interface{}, error) {
	payload, _ := json.Marshal(extPay)
	var url string
	if pmx == "t" {
		url = "https://checkout.kbb1.com/token/charge"
	} else if pmx == "e" {
		url = "https://checkout.kbb1.com/emv/charge"
	}
	resp, err := utils.PostJSON("POST", url, payload)
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

func (o *OrdersDB) renewOrder(ctx context.Context, orderID uint, pmx string) string {
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
	order := o.GetOrderByID(ctx, orderID)
	p := o.GetPaymentForOrderID(ctx, orderID)
	a := o.GetAccountForOrderID(ctx, orderID)

	pr, newp := o.createRequestPayByToken(ctx, a, order, p, null.NewString(pmx, true))
	resp, err := o.renewPaymentByToken(pr, pmx)
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
		o.flagOrderAsRenewed(ctx, orderID)
	} else {
		newp.PaymentStatus = null.NewString("failed", true)
		newp.Success = null.NewString("0", true)
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(newp)

	if len(toUpdateArgs) != 0 {
		updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, newp.ID),
			toUpdateArgs...)
		if err != nil {
			fmt.Errorf("problem updating payments: %w", err)
		}

		toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(newp)

		// update payments_pelecard table after payment
		_, pelecardErr := o.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, newp.ID),
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
	o.UpdateOrderAfterPayment(ctx, newp)
	return newp.Success.String
}

func (o *OrdersDB) flagOrderAsRenewed(ctx context.Context, orderID uint) {
	updateRes, err := o.Exec(ctx, `UPDATE orders 
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

func (o *OrdersDB) ChargeOrdersToRenew(ctx context.Context, pmx string) int {
	sqlQuery := `
	Select id from orders 
	Where ("Status" = 'paid' or "Status" = 'nosuccess')
	and "Type" = 'recurring'
	and "Flag" = 'torenew'
	`
	rows, err := o.Query(ctx, sqlQuery)

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
		status := o.renewOrder(ctx, uint(id), pmx)
		if status == "1" {
			count++
		} else {
			log.Printf("## Error with %v", id)
		}
	}
	return count
}

func (o *OrdersDB) FlagDuplicateOrders(ctx context.Context, ProductType string) int {
	req := `select "AccountID" as id, count(*) as "duplicate" 
from orders where "Status" = 'paid' 
group by "AccountID" 
having count(*) > 1
order by duplicate desc`

	// rows, err := DB.Raw(req).Rows() // (*sql.Rows, error)
	rows, err := o.Query(ctx, req)
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
		o.flagOrdersByAccountID(ctx, id, "duplicate")
		count++
	}
	return count
}

func (o *OrdersDB) CreateV2Order(ctx context.Context, order Order) (int, error) {
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	if len(createQueryArgs) == 0 {
		return 0, common.ErrInvalidBody
	}

	var ID int
	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(
		&ID,
	); err != nil {
		return 0, err
	}

	o.emitEvent(ctx, events.TypeCreateOrder, map[string]interface{}{"order_id": ID})

	return ID, nil
}

func (o *OrdersDB) SoftDeleteOrderByID(ctx context.Context, orderID int) error {
	_, err := o.Exec(ctx, "UPDATE orders SET deleted_at = $1 WHERE id = $2", time.Now(), orderID)
	if err != nil {
		return err
	}
	o.emitEvent(ctx, events.TypeDeleteOrder, map[string]interface{}{"order_id": orderID})
	return nil
}

func (o *OrdersDB) PatchOrderByID(ctx context.Context, order Order, orderId int) error {
	toUpdate, toUpdateArgs := prepareOrderUpdateQuery(order)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidBody
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, orderId), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("problem updating order: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return fmt.Errorf("order not updated as no rows affected")
	}

	o.emitEvent(ctx, events.TypeUpdateOrder, map[string]interface{}{"order_id": orderId})

	return nil
}

func (o *OrdersDB) addFlagToOrder(ctx context.Context, oid uint, flag string) {
	order := o.GetOrderByID(ctx, oid)
	order.Flag = null.NewString(flag, true)

	if order.ID == 0 {
		fmt.Println("order not found")
		return
	}

	toUpdate, toUpdateArgs := prepareOrderUpdateQuery(order)

	if len(toUpdateArgs) != 0 {
		updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, order.ID),
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

func (o *OrdersDB) flagOrdersByAccountID(ctx context.Context, aid int, flag string) int {
	req := `select id from orders where "AccountID" = $1 and "Status" = 'paid'`
	rows, err := o.Query(ctx, req, aid)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	for rows.Next() {
		rows.Scan(&id)
		o.addFlagToOrder(ctx, uint(id), flag)
		count++
	}
	return count

}

func (o *OrdersDB) GetAllOrders(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time, productType string,
	currency string, status string, organisation string, email string, accountID int, evaluateMembership string,
	orderByPaymentDate string) (*[]Order, error) {

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

	rows, err := o.Query(ctx, `SELECT 
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
		args = append(args, req.RecuringFreq.Int)
	}
	if req.AccountID.Valid {
		createStrings = append(createStrings, `"AccountID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AccountID.Int)
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
		args = append(args, req.Quantity.Int)
	}
	if req.AmountItem.Valid {
		createStrings = append(createStrings, "amount_item")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AmountItem.Int)
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
		args = append(args, req.RecuringFreq.Int)
	}
	if req.AccountID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"AccountID"=$%d`, len(updateStrings)+1))
		args = append(args, req.AccountID.Int)
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
		args = append(args, req.Quantity.Int)
	}
	if req.AmountItem.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`amount_item=$%d`, len(updateStrings)+1))
		args = append(args, req.AmountItem.Int)
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
		orderBy.WriteString(fmt.Sprintf(" ORDER BY o.\"PaymentDate\" %s, o.starting_date %s", orderByPaymentDate, orderByPaymentDate))
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

func (o *OrdersDB) HasPaidMembership(ctx context.Context, email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = $1
and o."AccountID" = a.id
and o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success' or o."Status" = 'nosuccess')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	if err := o.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}

func (o *OrdersDB) HasTicket(ctx context.Context, email string) bool {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = $1
and o."AccountID" = a.id
and (o."ProductType" = 'jan2022ticket' or
     o."ProductType" = 'jan2022ticket10' or
     o."ProductType" = 'jan2022ticket30')
and (o."Status" = 'paid' or o."Status" = 'success')
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.Raw(query, email).Scan(&r)
	if err := o.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}
