package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
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

	accountID, err := o.GetOrCreateAccount(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("o.GetOrCreateAccount: %w", err)
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

func (o *OrdersDB) UpdateOrdersToken(ctx context.Context, req RequestUpdateToken) error {
	account, err := o.GetAccountForOrderID(ctx, uint(req.OrderId))
	if err != nil {
		return fmt.Errorf("GetAccountForOrderID: %w", err)
	}

	cardDetails := CardDetails{
		AccountID: null.IntFrom(account.ID),
		CCNumber:  null.StringFrom(req.CardNumber),
		CCExpDate: null.StringFrom(req.CardExp),
		Active:    null.BoolFrom(true),
		Token:     null.StringFrom(req.Token),
	}
	cardID, err := o.CreateCardDetailsAndGetId(ctx, cardDetails)
	if err != nil {
		return fmt.Errorf("CreateCardDetailsAndGetId: %w", err)
	}

	if err := o.PatchOrderByID(ctx, Order{CardDetailsId: null.IntFrom(cardID)}, req.OrderId); err != nil {
		return fmt.Errorf("PatchOrderByID: %w", err)
	}

	return nil
}

func (o *OrdersDB) UpdateOrderAfterPayment(ctx context.Context, p Payment) error {
	var order Order

	if err := o.QueryRow(ctx, `SELECT id, "ProductType", "AccountID", "OrderLanguage" FROM orders WHERE id=$1`, p.OrderID.Int).
		Scan(&order.ID, &order.ProductType, &order.AccountID, &order.OrderLanguage); err != nil {
		return fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	if p.Success.String == "1" {
		order.Status = null.NewString(common.OrderStatusPaid, true)
		order.PaymentDate = null.NewTime(time.Now(), true)

		_, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1, "PaymentDate"=$2, updated_at=$3 WHERE id = $4`,
			order.Status.String, order.PaymentDate.Time, time.Now(), p.OrderID.Int)
		if err != nil {
			return fmt.Errorf("o.Exec [success]: %w", err)
		}
	} else {
		order.Status = null.NewString(common.OrderStatusNoSuccess, true)
		_, err := o.Exec(ctx, `UPDATE orders SET "Status"=$1, updated_at=$2 WHERE id = $3`,
			order.Status.String, time.Now(), p.OrderID.Int)
		if err != nil {
			return fmt.Errorf("o.Exec [%s]: %w", common.OrderStatusNoSuccess, err)
		}
	}

	o.emitEvent(ctx, events.TypeUpdateOrder, map[string]interface{}{"order_id": order.ID})

	return nil
}

func (o *OrdersDB) GetOrderByID(ctx context.Context, orderID uint) (*Order, error) {
	var order Order
	var amount null.String

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
	card_details_id,
	quantity,
	amount_item,
	created_at,
	updated_at,
	deleted_at 
	FROM orders WHERE id=$1`, orderID).Scan(
		&order.ID, &order.Type, &order.ProductType, &order.RecuringFreq, &order.AccountID, &order.Organization, &amount,
		&order.Currency, &order.Status, &order.OrderLanguage, &order.PaymentDate, &order.StartingDate, &order.Flag, &order.CardDetailsId, &order.Quantity, &order.AmountItem,
		&order.CreatedAt, &order.UpdatedAt, &order.DeletedAt,
	); err != nil {
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	if !amount.Valid {
		fmt.Println("amount is null, expected float")
		return nil, fmt.Errorf("amount is null, expected float")
	}
	value, err := strconv.ParseFloat(amount.String, 64)
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
	currency string, status string, organisation string, email string, accountID int, keycloakID string, evaluateMembership string,
	orderByPaymentDate string) (*[]Order, error) {
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery, queryBuildErr := buildAndGetOrdersWhereQuery(fromDate, toDate, productType, currency, status,
		organisation, email, accountID, keycloakID, evaluateMembership, orderByPaymentDate)

	if queryBuildErr != nil {
		return nil, fmt.Errorf("buildAndGetOrdersWhereQuery: %w", queryBuildErr)
	}

	fromQuery := " FROM orders as o"
	if email != "" {
		fromQuery = fromQuery + ", accounts as a"
	}

	query := `SELECT 
		o.id, o."Type", o."ProductType", o."RecuringFreq", o."AccountID", o."Organization", o."Amount", 
		"Currency", o."Status", o."OrderLanguage", o."PaymentDate", o.starting_date, o."SKU", o."Note", o."Flag",o.card_details_id, o.quantity, o.amount_item,
		 o.created_at, o.updated_at, o.deleted_at
	` + fromQuery + whereQuery + orderByQuery + limitOffsetString

	// utils.LogFor(ctx).Info("GetAllOrders.query", slog.String("sql", query))
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
			&d.Currency, &d.Status, &d.OrderLanguage, &d.PaymentDate, &d.StartingDate, &d.SKU, &d.Note, &d.Flag, &d.CardDetailsId, &d.Quantity, &d.AmountItem,
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
	if req.CardDetailsId.Valid {
		createStrings = append(createStrings, `"card_details_id"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CardDetailsId.Int)
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
	if req.CardDetailsId.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"card_details_id"=$%d`, len(updateStrings)+1))
		args = append(args, req.CardDetailsId.Int)
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

func buildAndGetOrdersWhereQuery(fromDate string, dateTo *time.Time, productType string, currency string, status string,
	organisation string, email string, accountID int, keycloakID string, evaluateMembership string, orderByPaymentDate string) (string, string, error) {

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
			whereCondition.WriteString(
				" AND (o.\"Status\" = '" + common.OrderStatusPaid +
					"' OR o.\"Status\" = '" + common.OrderStatusSuccess +
					"' OR o.\"Status\" = '" + common.OrderStatusNoSuccess +
					"' OR o.\"Status\" = '" + common.OrderStatusCancelled + "')")
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
and o."ProductType" = '` + common.ProductTypeGlobalMembership +
		`' and (o."Status" = '` + common.OrderStatusPaid +
		`' or o."Status" = '` + common.OrderStatusSuccess +
		`' or o."Status" = '` + common.OrderStatusNoSuccess + `')`

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

// GetTokensForOrders batch fetches the latest pelecard_token for multiple orders.
// Uses the same fallback logic as createRequestPayByToken:
// 1. First tries to get token from CardDetails (if order has CardDetailsId and card is active)
// 2. Falls back to the first Payment made on order creation
// Returns a map of orderID -> token (empty string if no token found)
func (o *OrdersDB) GetTokensForOrders(ctx context.Context, orderIDs []int) (map[int]string, error) {
	if len(orderIDs) == 0 {
		return make(map[int]string), nil
	}

	result := make(map[int]string)
	// Initialize all orderIDs with empty string
	for _, id := range orderIDs {
		result[id] = ""
	}

	// Build the IN clause with placeholders
	placeholders := make([]string, len(orderIDs))
	args := make([]interface{}, len(orderIDs))
	for i, id := range orderIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	// Step 1: Get orders with their CardDetailsId
	ordersQuery := fmt.Sprintf(`
		SELECT id, card_details_id
		FROM orders
		WHERE id IN (%s)
	`, strings.Join(placeholders, ", "))

	orderRows, err := o.Query(ctx, ordersQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("o.Query [orders]: %w", err)
	}
	defer orderRows.Close()

	type orderCardInfo struct {
		orderID       int
		cardDetailsID null.Int
	}
	var ordersWithCards []orderCardInfo
	var ordersWithoutCards []int

	for orderRows.Next() {
		var orderID int
		var cardDetailsID null.Int
		if err := orderRows.Scan(&orderID, &cardDetailsID); err != nil {
			return nil, fmt.Errorf("rows.Scan [orders]: %w", err)
		}
		if cardDetailsID.Valid && cardDetailsID.Int > 0 {
			ordersWithCards = append(ordersWithCards, orderCardInfo{orderID: orderID, cardDetailsID: cardDetailsID})
		} else {
			ordersWithoutCards = append(ordersWithoutCards, orderID)
		}
	}

	if err := orderRows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err [orders]: %w", err)
	}

	// Step 2: Get tokens from CardDetails for orders that have CardDetailsId
	if len(ordersWithCards) > 0 {
		cardPlaceholders := make([]string, len(ordersWithCards))
		cardArgs := make([]interface{}, len(ordersWithCards))
		for i, oc := range ordersWithCards {
			cardPlaceholders[i] = fmt.Sprintf("$%d", i+1)
			cardArgs[i] = oc.cardDetailsID.Int
		}

		cardDetailsQuery := fmt.Sprintf(`
			SELECT cd.id, cd.token, o.id as order_id
			FROM card_details cd
			INNER JOIN orders o ON o.card_details_id = cd.id
			WHERE cd.id IN (%s)
				AND cd.active = true
				AND cd.token IS NOT NULL
				AND cd.token != ''
				AND cd.deleted_at IS NULL
		`, strings.Join(cardPlaceholders, ", "))

		cardRows, err := o.Query(ctx, cardDetailsQuery, cardArgs...)
		if err != nil {
			return nil, fmt.Errorf("o.Query [card_details]: %w", err)
		}
		defer cardRows.Close()

		ordersWithCardTokens := make(map[int]bool)
		for cardRows.Next() {
			var cardID int
			var token null.String
			var orderID int
			if err := cardRows.Scan(&cardID, &token, &orderID); err != nil {
				return nil, fmt.Errorf("rows.Scan [card_details]: %w", err)
			}
			if token.Valid && token.String != "" {
				result[orderID] = token.String
				ordersWithCardTokens[orderID] = true
			}
		}

		if err := cardRows.Err(); err != nil {
			return nil, fmt.Errorf("rows.Err [card_details]: %w", err)
		}

		// Collect orders that need fallback (have CardDetailsId but no valid token)
		for _, oc := range ordersWithCards {
			if !ordersWithCardTokens[oc.orderID] {
				ordersWithoutCards = append(ordersWithoutCards, oc.orderID)
			}
		}
	}

	// Step 3: Fallback to payments table for orders without CardDetails or without valid card tokens
	if len(ordersWithoutCards) > 0 {
		paymentPlaceholders := make([]string, len(ordersWithoutCards))
		paymentArgs := make([]interface{}, len(ordersWithoutCards))
		for i, id := range ordersWithoutCards {
			paymentPlaceholders[i] = fmt.Sprintf("$%d", i+1)
			paymentArgs[i] = id
		}

		// Use DISTINCT ON to get the first payment for each order (matching GetPaymentForOrderID logic)
		// GetPaymentForOrderID filters by "PaymentStatus"='success' and uses natural order (by id)
		paymentsQuery := fmt.Sprintf(`
			SELECT DISTINCT ON ("OrderID") "OrderID", pelecard_token
			FROM payments
			WHERE "OrderID" IN (%s)
				AND "PaymentStatus" = 'success'
			ORDER BY "OrderID", id ASC
		`, strings.Join(paymentPlaceholders, ", "))

		paymentRows, err := o.Query(ctx, paymentsQuery, paymentArgs...)
		if err != nil {
			return nil, fmt.Errorf("o.Query [payments]: %w", err)
		}
		defer paymentRows.Close()

		for paymentRows.Next() {
			var orderID int
			var token null.String
			if err := paymentRows.Scan(&orderID, &token); err != nil {
				return nil, fmt.Errorf("rows.Scan [payments]: %w", err)
			}
			// Only set if we don't already have a token from CardDetails
			if token.Valid && result[orderID] == "" {
				result[orderID] = token.String
			}
		}

		if err := paymentRows.Err(); err != nil {
			return nil, fmt.Errorf("rows.Err [payments]: %w", err)
		}
	}

	return result, nil
}

// UpdateOrdersUserKeyFromAccounts updates orders.userkey from accounts.UserKey
func (o *OrdersDB) UpdateOrdersUserKeyFromAccounts(ctx context.Context) error {
	query := `
		UPDATE orders 
		SET userkey = accounts."UserKey" 
		FROM accounts 
		WHERE orders."AccountID" = accounts.id
	`
	_, err := o.Exec(ctx, query)
	return err
}

// GetPaidOrdersCount returns the count of paid orders for a specific month/year
// lastDay should be the last day of the month (e.g., from period.GetEndOfMonth())
func (o *OrdersDB) GetPaidOrdersCount(ctx context.Context, year, month int, lastDay time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM orders
		WHERE "ProductType" = $1
		AND "Status" = $2
		AND "PaymentDate" >= $3
		AND "PaymentDate" <= $4
	`

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)

	var count int64
	err := o.QueryRow(ctx, query, common.ProductTypeGlobalMembership, common.OrderStatusPaid, startDate, lastDay).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query row scan: %w", err)
	}
	return count, nil
}

// GetOrdersToSkipDouble returns userkeys that have multiple paid/cancelled orders in the specified month
func (o *OrdersDB) GetOrdersToSkipDouble(ctx context.Context, year, month int, lastDay time.Time) ([]string, error) {
	query := `
		SELECT userkey
		FROM orders
		WHERE ("Status" = $1 OR "Status" = $2)
		AND "ProductType" = $3
		AND "PaymentDate" >= $4
		AND "PaymentDate" <= $5
		GROUP BY userkey
		HAVING COUNT(userkey) > 1
	`

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)

	rows, err := o.Query(ctx, query, common.OrderStatusPaid, common.OrderStatusCancelled, common.ProductTypeGlobalMembership, startDate, lastDay)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var userkeys []string
	for rows.Next() {
		var userkey null.String
		if err := rows.Scan(&userkey); err != nil {
			return nil, fmt.Errorf("rows scan: %w", err)
		}
		if userkey.Valid {
			userkeys = append(userkeys, userkey.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return userkeys, nil
}

// GetOrdersToSkipFresh returns userkeys who already paid or cancelled this month
func (o *OrdersDB) GetOrdersToSkipFresh(ctx context.Context, year, month int, lastDay time.Time) ([]string, error) {
	query := `
		SELECT DISTINCT userkey
		FROM orders
		WHERE ("Status" = $1 OR "Status" = $2)
		AND "ProductType" = $3
		AND ("Flag" = '' OR "Flag" IS NULL)
		AND "PaymentDate" >= $4
		AND "PaymentDate" <= $5
	`

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)

	rows, err := o.Query(ctx, query, common.OrderStatusPaid, common.OrderStatusCancelled, common.ProductTypeGlobalMembership, startDate, lastDay)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var userkeys []string
	for rows.Next() {
		var userkey null.String
		if err := rows.Scan(&userkey); err != nil {
			return nil, fmt.Errorf("rows scan: %w", err)
		}
		if userkey.Valid {
			userkeys = append(userkeys, userkey.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	return userkeys, nil
}

// SkipOrdersByUserKey sets Flag='skip' for all orders with the given userkey that are currently flagged as 'torenew'
func (o *OrdersDB) SkipOrdersByUserKey(ctx context.Context, userkey string) (int, error) {
	query := `
		UPDATE orders
		SET "Flag" = $1
		WHERE "Flag" = $2
		AND userkey = $3
	`

	result, err := o.Exec(ctx, query, common.OrderFlagSkip, common.OrderFlagToRenew, userkey)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	rowsAffected := result.RowsAffected()
	return int(rowsAffected), nil
}
