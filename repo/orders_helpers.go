package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

func (o *OrdersDB) UpdateOrderStatusByOrderID(ctx context.Context, oid int, status string) error {
	_, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1 WHERE id=$2`, status, oid)
	return err
}

func (o *OrdersDB) CreateOrderViaTransaction(ctx context.Context, req RequestOrder) (*Order, error) {
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

		AccountType: null.NewString("personal", true),
		UserKey:     req.UserKey,
	}

	accountID, err := o.CreateOrUpdateAccount(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("o.CreateOrUpdateAccount: %w", err)
	}

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
		Status:        null.NewString("pending", true),
		OrderLanguage: req.OrderLanguage,
		AccountID:     null.IntFrom(accountID),
	}

	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	err = o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).
		Scan(&order.ID)
	if err != nil {
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	o.emitEvent(ctx, events.TypeCreateOrder, map[string]interface{}{"order_id": order.ID})

	return &order, nil
}

func (o *OrdersDB) UpdateOrderAfterPayment(ctx context.Context, p Payment) error {
	var order Order

	if err := o.QueryRow(ctx, `SELECT id, "ProductType", "AccountID", "OrderLanguage" FROM orders WHERE id=$1`, p.OrderID.Int).
		Scan(&order.ID, &order.ProductType, &order.AccountID, &order.OrderLanguage); err != nil {
		return fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	if p.Success.String == "1" {
		order.Status = null.NewString("paid", true)
		order.PaymentDate = null.NewTime(time.Now(), true)

		_, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1, "PaymentDate"=$2, updated_at=$3 WHERE id = $4`,
			order.Status.String, order.PaymentDate.Time, time.Now(), p.OrderID.Int)
		if err != nil {
			return fmt.Errorf("o.Exec [success]: %w", err)
		}
	} else {
		order.Status = null.NewString("nosuccess", true)
		_, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1, updated_at=$2 WHERE id = $3`,
			order.Status.String, time.Now(), p.OrderID.Int)
		if err != nil {
			return fmt.Errorf("o.Exec [nosuccess]: %w", err)
		}
	}

	o.emitEvent(ctx, events.TypeUpdateOrder, map[string]interface{}{"order_id": order.ID})

	return nil
}
func (o *OrdersDB) GetOrderByID(ctx context.Context, orderID uint) (*Order, error) {
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
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	value, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		fmt.Println("error converting amount string to float")
		return nil, fmt.Errorf("strconv.ParseFloat(amount): %w", err)
	}

	order.Amount = null.Float64From(value)

	return &order, nil
}

// Get Payment
func (o *OrdersDB) GetPaymentForOrderID(ctx context.Context, orderID uint) (*Payment, error) {
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
		return nil, err
	}

	return &p, nil
}

func (o *OrdersDB) GetAccountForOrderID(ctx context.Context, orderID uint) (*Account, error) {
	order, err := o.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("o.GetOrderByID: %w", err)
	}

	var a Account
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
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	return &a, nil
}

func (o *OrdersDB) createRequestPayByToken(c context.Context, a *Account, order *Order, p *Payment, pmx null.String) (*RequestPayment, *Payment, error) {
	newp, err := o.createPendingPayment(c, order, pmx)
	if err != nil {
		return nil, nil, fmt.Errorf("o.createPendingPayment: %w", err)
	}

	newp.PelecardToken = p.PelecardToken
	newp.AuthNo = p.AuthNo

	extPay := RequestPayment{
		UserKey: newp.Ordkey.String,

		GoodURL:    "http://ec41a043fda1.ngrok.io/pelecard/good",
		ErrorURL:   "http://ec41a043fda1.ngrok.io/pelecard/error",
		CancelURL:  "http://ec41a043fda1.ngrok.io/pelecard/cancel",
		ApprovalNo: p.AuthNo.String,
		Token:      p.PelecardToken.String,

		Name:         a.FirstName.String + " " + a.LastName.String,
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

	return &extPay, newp, nil
}

func (o *OrdersDB) renewPaymentByToken(ctx context.Context, extPay *RequestPayment, pmx string) (interface{}, error) {
	var url string
	if pmx == "t" {
		url = "https://checkout.kbb1.com/token/charge"
	} else if pmx == "e" {
		url = "https://checkout.kbb1.com/emv/charge"
	}

	payload, _ := json.Marshal(extPay)
	resp, err := utils.PostJSON("POST", url, payload)
	if err != nil {
		return nil, fmt.Errorf("utils.PostJSON: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("io.ReadAll: %w", err)
	}

	utils.LogFor(ctx).Info("payment response", slog.Group("response",
		slog.String("status", resp.Status),
		slog.Any("headers", resp.Header),
		slog.String("body", string(body))))

	var i interface{}
	if err := json.Unmarshal(body, &i); err != nil {
		return nil, fmt.Errorf("json.Unmarshal: %w", err)
	}

	return i, nil
}

func (o *OrdersDB) renewOrder(ctx context.Context, orderID uint, pmx string) (string, error) {
	/*
		get account by order
		if no token in account
		get payment for order
		extract token
		make payment by token
		if payment successfull (handled in /pelecard good then ... )
		TODO update account with token
		TODO update order
	*/

	order, err := o.GetOrderByID(ctx, orderID)
	if err != nil {
		return "", fmt.Errorf("o.GetOrderByID: %w", err)
	}

	p, err := o.GetPaymentForOrderID(ctx, orderID)
	if err != nil {
		return "", fmt.Errorf("o.GetPaymentForOrderID: %w", err)
	}

	a, err := o.GetAccountForOrderID(ctx, orderID)
	if err != nil {
		return "", fmt.Errorf("o.GetAccountForOrderID: %w", err)
	}

	pr, newp, err := o.createRequestPayByToken(ctx, a, order, p, null.StringFrom(pmx))
	if err != nil {
		return "", fmt.Errorf("o.createRequestPayByToken: %w", err)
	}

	resp, payErr := o.renewPaymentByToken(ctx, pr, pmx)
	if payErr != nil {
		// Note: we don't return here with the payErr as we have to update the failure in DB later on
		newp.PaymentStatus = null.StringFrom("failed")
		newp.Success = null.StringFrom("0")

		utils.LogFor(ctx).Error("external payment error [renew]: %w", payErr)
		hub := utils.SentryFor(ctx)
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("order_id", order.ID)
			scope.SetExtra("payment_id", p.ID)
			hub.CaptureException(payErr)
		})
	} else {
		answers := resp.(map[string]interface{})
		if answers["status"].(string) == "success" {
			newp.PaymentStatus = null.StringFrom("success")
			newp.Success = null.StringFrom("1")
			if err := o.flagOrderAsRenewed(ctx, orderID); err != nil {
				return newp.Success.String, fmt.Errorf("o.flagOrderAsRenewed: %w", err)
			}
		} else {
			newp.PaymentStatus = null.StringFrom("failed")
			newp.Success = null.StringFrom("0")
		}
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(*newp)
	_, err = o.Exec(ctx,
		fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, newp.ID), toUpdateArgs...)
	if err != nil {
		return newp.Success.String, fmt.Errorf("o.Exec [update payment]: %w", err)
	}

	toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(*newp)
	_, err = o.Exec(ctx,
		fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, newp.ID),
		toUpdateArgsPeleCard...)
	if err != nil {
		return newp.Success.String, fmt.Errorf("o.Exec [update pelecard_payment]: %w", err)
	}

	err = o.UpdateOrderAfterPayment(ctx, *newp)
	if err != nil {
		return newp.Success.String, fmt.Errorf("o.UpdateOrderAfterPayment: %w", err)
	}

	// All DB interaction is fine yet we still had an error with the payment gateway
	if payErr != nil {
		return newp.Success.String, fmt.Errorf("external payment error: %w", payErr)
	}

	return newp.Success.String, nil
}

func (o *OrdersDB) flagOrderAsRenewed(ctx context.Context, orderID uint) error {
	_, err := o.Exec(ctx, `UPDATE orders SET "Flag"=$1, updated_at=$2 WHERE id = $3`, "renewed", time.Now(), orderID)
	return err
}

func (o *OrdersDB) ChargeOrdersToRenew(ctx context.Context, pmx string) (int, error) {
	sqlQuery := `
	Select id from orders 
	Where ("Status" = 'paid' or "Status" = 'nosuccess')
	and "Type" = 'recurring'
	and "Flag" = 'torenew'
	`

	rows, err := o.Query(ctx, sqlQuery)
	if err != nil {
		return 0, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var count int
	var id int
	count = 0
	for rows.Next() {
		err := rows.Scan(&id)
		if err != nil {
			return count, fmt.Errorf("rows.Scan: %w", err)
		}

		utils.LogFor(ctx).Info("Renewing order", slog.Int("order_id", id))
		status, err := o.renewOrder(ctx, uint(id), pmx)
		if err != nil {
			utils.LogFor(ctx).Error("renew order error", slog.Int("order_id", id), slog.Any("err", err))
		} else {
			if status == "1" {
				count++
			} else {
				utils.LogFor(ctx).Info("renew order failed", slog.Int("order_id", id), slog.String("status", status))
			}
		}

	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("rows.Err: %w", err)
	}

	return count, nil
}

func (o *OrdersDB) CreateV2Order(ctx context.Context, order Order) (int, error) {
	createString, numString, createQueryArgs := prepareOrderCreateQuery(order)
	if len(createQueryArgs) == 0 {
		return 0, common.ErrInvalidValues
	}

	var ID int
	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO orders (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&ID); err != nil {
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
		return common.ErrInvalidValues
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, orderId), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("problem updating order: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	o.emitEvent(ctx, events.TypeUpdateOrder, map[string]interface{}{"order_id": orderId})

	return nil
}

func (o *OrdersDB) GetAllOrders(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time, productType string,
	currency string, status string, organisation string, email string, accountID int, evaluateMembership string,
	orderByPaymentDate string, keycloakID string) (*[]Order, error) {
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery, queryBuildErr := buildAndGetOrdersWhereQuery(fromDate, toDate, productType, currency, status, organisation, email, accountID, keycloakID, evaluateMembership, orderByPaymentDate)

	if queryBuildErr != nil {
		return nil, fmt.Errorf("buildAndGetOrdersWhereQuery: %w", queryBuildErr)
	}

	fromQuery := " FROM orders as o"
	if email != "" {
		fromQuery = fromQuery + ", accounts as a"
	}

	query := `SELECT 
		o.id, o."Type", o."ProductType", o."RecuringFreq", o."AccountID", o."Organization", o."Amount", 
		"Currency", o."Status", o."OrderLanguage", o."PaymentDate", o.starting_date, o."SKU", o."Note", o."Flag", o.quantity, o.amount_item,
		 o.created_at, o.updated_at, o.deleted_at
	` + fromQuery + whereQuery + orderByQuery + limitOffsetString

	utils.LogFor(ctx).Info("GetAllOrders.query", slog.String("sql", query))
	rows, err := o.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		var d Order
		err := rows.Scan(
			&d.ID, &d.Type, &d.ProductType, &d.RecuringFreq, &d.AccountID, &d.Organization, &d.Amount,
			&d.Currency, &d.Status, &d.OrderLanguage, &d.PaymentDate, &d.StartingDate, &d.SKU, &d.Note, &d.Flag, &d.Quantity, &d.AmountItem,
			&d.CreatedAt, &d.UpdatedAt, &d.DeletedAt)
		if err != nil {
			return &orders, fmt.Errorf("rows.Scan: %w", err)
		}
		orders = append(orders, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return &orders, nil

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

func buildAndGetOrdersWhereQuery(fromDate string, dateTo *time.Time, productType string, currency string, status string, organisation string, email string, accountID int, keycloakID string, evaluateMembership string, orderByPaymentDate string) (string, string, error) {

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
	if keycloakID != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND o.userkey = '%s'", keycloakID))
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
		orderBy.WriteString(fmt.Sprintf(" ORDER BY COALESCE(o.\"PaymentDate\", o.created_at) %s, o.starting_date %s",
			orderByPaymentDate, orderByPaymentDate))
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

func (o *OrdersDB) HasPaidMembership(ctx context.Context, email string) (bool, error) {
	query := `
select count(o.*) as total
from orders as o, accounts as a
where a."Email" = $1
and o."AccountID" = a.id
and o."ProductType" = 'globalmembership'
and (o."Status" = 'paid' or o."Status" = 'success' or o."Status" = 'nosuccess')
`
	count, err := o.count(ctx, query, email)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (o *OrdersDB) HasTicket(ctx context.Context, email string) (bool, error) {
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

	count, err := o.count(ctx, query, email)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
