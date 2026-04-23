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

func (o *OrdersDB) GetPaymentByID(ctx context.Context, id int) (*Payment, error) {
	var (
		pay      Payment
		addQuery string
	)
	if err := o.QueryRow(ctx, `SELECT
	id, created_at, updated_at, deleted_at, "Amount", "Currency", "PaymentStatus", "PaymentType", "OrderID", "ParamX",
	"Ordkey", "AuthNo", confirmation_key, success, pelecard_token, "TransactionID", "ErrorMsg", "CardHebrewName",
	"CCAbroadCard", "CCBrand", "CCCompanyClearer", "CCCompanyIssuer", credit_type, "CCExpDate", "CCNumber", "DebitCode",
	"DebitCurrency", "DebitTotal", "DebitType", "FirstPaymentTotal", "FixedPaymentTotal", "TotalPayments", j_param,
	"TransactionInitTime", "TransactionUpdateTime", "VoucherID", pricing_version from payments where id = $1`+addQuery, id).Scan(
		&pay.ID, &pay.CreatedAt, &pay.UpdatedAt, &pay.DeletedAt, &pay.Amount, &pay.Currency, &pay.PaymentStatus,
		&pay.PaymentType, &pay.OrderID, &pay.ParamX, &pay.Ordkey, &pay.AuthNo, &pay.ConfirmationKey, &pay.Success,
		&pay.PelecardToken, &pay.TransactionID, &pay.ErrorMsg, &pay.CardHebrewName, &pay.CCAbroadCard, &pay.CCBrand,
		&pay.CCCompanyClearer, &pay.CCCompanyIssuer, &pay.CreditType, &pay.CCExpDate, &pay.CCNumber, &pay.DebitCode,
		&pay.DebitCurrency, &pay.DebitTotal, &pay.DebitType, &pay.FirstPaymentTotal, &pay.FixedPaymentTotal,
		&pay.TotalPayments, &pay.JParam, &pay.TransactionInitTime, &pay.TransactionUpdateTime, &pay.VoucherID, &pay.PricingVersion); err != nil {
		return nil, err
	}

	return &pay, nil
}

func (o *OrdersDB) SoftDeletePayment(ctx context.Context, paymentID int) error {
	_, err := o.Exec(ctx, "UPDATE payments SET deleted_at = $1 WHERE id = $2", time.Now(), paymentID)
	if err != nil {
		return err
	}
	o.emitEvent(ctx, events.TypeDeletePayment, map[string]interface{}{"payment_id": paymentID})
	return nil
}

func (o *OrdersDB) GetPaymentActivities(ctx context.Context, email string, productType string, paymentType string, skip int, limit int) ([]PaymentActivitiesRes, error) {
	userDbWhereQuery, orderByQuery := buildAndGetWherePaymentActQuery(email, productType, paymentType)

	rows, err := o.Query(ctx, `SELECT p.created_at,  p."Amount", p."PaymentType",  p."OrderID", 
	p."ParamX", p."PaymentStatus", p."CCNumber", p."CCExpDate", 
	o."ProductType", o."Type", o."Currency",
	a."FirstName", a."LastName", a."Email", a."Country" 
	from payments as p, orders as o, accounts as a`+
		userDbWhereQuery+orderByQuery+" LIMIT $1 OFFSET $2", limit, skip)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	PaymentActivities := []PaymentActivitiesRes{}
	for rows.Next() {
		var p PaymentActivitiesRes
		err := rows.Scan(&p.CreatedAt, &p.Amount, &p.PaymentType, &p.OrderID, &p.ParamX, &p.PaymentStatus, &p.CCNumber,
			&p.CCExpDate, &p.ProductType, &p.Type, &p.Currency, &p.FirstName, &p.LastName, &p.Email, &p.Country)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}

		PaymentActivities = append(PaymentActivities, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return PaymentActivities, nil
}

func (o *OrdersDB) GetAllPayments(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time,
	paymentType string, paymentStatus string, orderType string, email string, accountID int, paymentsWithToken string,
	intOrderID int, orderByCreatedAt string) ([]Payment, error) {

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery, err := buildAndGetPaymentsWhereQuery(fromDate, toDate, paymentType, paymentStatus,
		orderType, email, accountID, paymentsWithToken, intOrderID, orderByCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("buildAndGetPaymentsWhereQuery: %w", err)
	}

	fromQuery := " FROM payments as p"
	if email != "" || accountID != 0 || orderType != "" {
		fromQuery = fromQuery + ", orders as o"
		if email != "" || accountID != 0 {
			fromQuery = fromQuery + ", accounts as a"
		}
	}

	rows, err := o.Query(ctx, `SELECT 
	p.id, p.created_at, p.updated_at, p.deleted_at, p."Amount", p."Currency", p."PaymentStatus", p."PaymentType", 
	p."OrderID", p."ParamX", p."Ordkey", p."AuthNo", p.confirmation_key, p.success, p.pelecard_token, p."TransactionID", 
	p."ErrorMsg", p."CardHebrewName", p."CCAbroadCard", p."CCBrand", p."CCCompanyClearer", p."CCCompanyIssuer", 
	p.credit_type, p."CCExpDate", p."CCNumber", p."DebitCode", p."DebitCurrency", p."DebitTotal", p."DebitType", 
	p."FirstPaymentTotal", p."FixedPaymentTotal", p."TotalPayments", p.j_param, p."TransactionInitTime", 
	p."TransactionUpdateTime", p."VoucherID"`+fromQuery+whereQuery+orderByQuery+limitOffsetString)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	payments := []Payment{}
	for rows.Next() {
		var d Payment
		err := rows.Scan(
			&d.ID, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &d.Amount, &d.Currency, &d.PaymentStatus, &d.PaymentType,
			&d.OrderID, &d.ParamX, &d.Ordkey, &d.AuthNo, &d.ConfirmationKey, &d.Success, &d.PelecardToken,
			&d.TransactionID, &d.ErrorMsg, &d.CardHebrewName, &d.CCAbroadCard, &d.CCBrand, &d.CCCompanyClearer,
			&d.CCCompanyIssuer, &d.CreditType, &d.CCExpDate, &d.CCNumber, &d.DebitCode, &d.DebitCurrency, &d.DebitTotal,
			&d.DebitType, &d.FirstPaymentTotal, &d.FixedPaymentTotal, &d.TotalPayments, &d.JParam,
			&d.TransactionInitTime, &d.TransactionUpdateTime, &d.VoucherID)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		payments = append(payments, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return payments, nil
}

func (o *OrdersDB) GetTotalParticipationStatusCount(ctx context.Context, email string, productType string,
	paymentType string) (int, error) {
	var count int

	userDbWhereQuery, _ := buildAndGetWherePaymentActQuery(email, productType, paymentType)
	err := o.QueryRow(ctx, `SELECT COUNT(*) FROM payments as p, orders as o, accounts as a`+userDbWhereQuery).
		Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (o *OrdersDB) GetPaymentByEmail(ctx context.Context, email string) ([]PaymentByEmail, error) {
	rows, err := o.Query(ctx, `select p."OrderID", p.created_at, o."PaymentDate", o."Type", o."ProductType", p."Amount", 
	p."Currency", p."CCNumber", p."ParamX", p."PaymentStatus"
	from payments as p, orders as o, accounts as a
	where a."Email" = $1
	and a.id = o."AccountID"
	and o.id = p."OrderID"
	order by p.created_at desc`, email)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	paymentData := []PaymentByEmail{}
	for rows.Next() {
		var p PaymentByEmail
		err := rows.Scan(&p.OrderID, &p.CreatedAt, &p.PaymentDate, &p.Type, &p.ProductType, &p.Amount, &p.Currency, &p.CCNumber,
			&p.PaymentID, &p.PaymentStatus)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}

		paymentData = append(paymentData, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return paymentData, nil
}

func (o *OrdersDB) GetOfflinePayments(ctx context.Context, skip int, limit int, method string, orderByCreatedAt string) ([]*OfflinePayment, error) {
	fromQuery := " FROM payments_offline as p"
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery, err := buildAndGetOfflinePaymentsWhereQuery(method, orderByCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("buildAndGetOfflinePaymentsWhereQuery: %w", err)
	}

	rows, err := o.Query(ctx, `SELECT 
	p.id, p.created_at, p.updated_at, p.deleted_at, p.payment_method, p.receipt, p.extra_info, p.status, p.payment_id,
	p.properties`+fromQuery+whereQuery+orderByQuery+limitOffsetString)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	payments := make([]*OfflinePayment, 0)
	for rows.Next() {
		var p OfflinePayment
		err := rows.Scan(
			&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.PaymentMethod, &p.Receipt, &p.ExtraInfo, &p.Status,
			&p.PaymentID, &p.Properties)
		if err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}

		payments = append(payments, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return payments, nil
}

func (o *OrdersDB) CreatePayment(ctx context.Context, req RequestOrder, orderID int) (*Payment, error) {
	paymentStatus := common.PaymentStatusPending
	if req.PaymentStatus.IsValid() {
		paymentStatus = req.PaymentStatus.String
	}

	paymentType := common.PaymentTypePelecard
	if req.PaymentType.IsValid() {
		paymentType = req.PaymentType.String
	}

	p := Payment{
		Amount:        req.Amount,
		Currency:      req.Currency,
		PaymentType:   null.NewString(paymentType, true),
		OrderID:       null.NewInt(orderID, true),
		PaymentStatus: null.NewString(paymentStatus, true),
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)

	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&p.ID); err != nil {
		return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
	}

	if paymentType == common.PaymentTypeOffline {
		err := o.createOfflinePayment(ctx, req, p.ID, paymentStatus)
		if err != nil {
			return nil, fmt.Errorf("o.createOfflinePayment: %w", err)
		}
	}

	if paymentType == common.PaymentTypeHelpHaver {
		err := o.createHelpHaverPayment(ctx, req, p.ID, paymentStatus)
		if err != nil {
			return nil, fmt.Errorf("createHelpHaverPayment: %w", err)
		}
	}

	if paymentType == common.PaymentTypePelecard {
		err := o.createPelecardPayment(ctx, req, p.ID, p)
		if err != nil {
			return nil, fmt.Errorf("createPelecardPayment: %w", err)
		}
	}

	o.emitEvent(ctx, events.TypeCreatePayment, map[string]interface{}{"payment_id": p.ID})

	return &p, nil
}

func (o *OrdersDB) UpdatePayment(ctx context.Context, req RequestPaid) (*Payment, error) {
	if !req.Error.IsZero() {
		return nil, fmt.Errorf("req.Error: %s", req.Error.String)
	}

	orderid, err := strconv.ParseUint(strings.Split(req.UserKey.String, "-")[1], 10, 0)
	if err != nil {
		return nil, fmt.Errorf("strconv.ParseUint [user_key]: %w", err)
	}
	paymentid, err := strconv.ParseUint(strings.Split(req.ParamX.String, "-")[1], 10, 0)
	if err != nil {
		return nil, fmt.Errorf("strconv.ParseUint [additional_details_param_x]: %w", err)
	}

	var p Payment
	if err := o.QueryRow(ctx, `SELECT 
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
		return nil, fmt.Errorf("o.QueryRow.Scan [payment]: %w", err)
	}

	if req.Success.String == "1" {
		p.PaymentStatus = null.NewString("success", true)
		p.PaymentType = null.StringFrom(common.PaymentTypePelecard)
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
		p.PaymentType = null.StringFrom(common.PaymentTypePelecard)
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)
	_, err = o.Exec(ctx, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, uint(paymentid)), toUpdateArgs...)
	if err != nil {
		return nil, fmt.Errorf("o.Exec [update payment]: %w", err)
	}

	toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(p)
	_, pelecardErr := o.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard,
		uint(paymentid)), toUpdateArgsPeleCard...)
	if pelecardErr != nil {
		return nil, fmt.Errorf("o.Exec [update pelecard]: %w", err)
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": paymentid})

	return &p, nil
}

func (o *OrdersDB) createOfflinePayment(ctx context.Context, req RequestOrder, paymentID int, status string) error {
	createString, numString, createQueryArgs := prepareOfflinePaymentCreateQuery(req, paymentID, status)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.Exec(ctx, fmt.Sprintf(`INSERT INTO payments_offline (%s) VALUES (%s)`, createString, numString), createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) createPelecardPayment(ctx context.Context, req RequestOrder, paymentID int, p Payment) error {
	createString, numString, createQueryArgs := preparePelecardPaymentCreateQuery(p, paymentID)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.Exec(ctx, fmt.Sprintf(`INSERT INTO payments_pelecard (%s) VALUES (%s)`, createString, numString), createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) createHelpHaverPayment(ctx context.Context, req RequestOrder, paymentID int, status string) error {
	createString, numString, createQueryArgs := prepareHelpHaverPaymentCreateQuery(req, paymentID, status)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.Exec(ctx, fmt.Sprintf(`INSERT INTO payments_helphaver (%s) VALUES (%s)`, createString, numString), createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) UpdatePelecardPayment(ctx context.Context, req PaymentUpdate) error {
	toUpdate, toUpdateArgs := preparePelecardPaymentUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": req.PaymentID.Int})

	return nil
}

func (o *OrdersDB) UpdateOfflinePayment(ctx context.Context, req PaymentUpdate) error {
	toUpdate, toUpdateArgs := prepareOfflinePaymentUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE payments_offline SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": req.PaymentID.Int})

	return nil
}

func (o *OrdersDB) UpdateHelpHavePayment(ctx context.Context, req PaymentUpdate) error {
	toUpdate, toUpdateArgs := prepareHelpHaverPaymentUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE payments_helphaver SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": req.PaymentID.Int})

	return nil
}

func (o *OrdersDB) UpdateParentPaymentTableStatusAndReturnOrderId(ctx context.Context, status string, paymentID int) (int, error) {
	var orderId int
	if err := o.QueryRow(ctx, `UPDATE payments SET "PaymentStatus"=$1 WHERE id=$2 RETURNING "OrderID"`, status, paymentID).
		Scan(&orderId); err != nil {
		return 0, err
	}
	return orderId, nil
}

func preparePelecardPaymentCreateQuery(req Payment, paymentID int) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if paymentID != 0 {
		createStrings = append(createStrings, "payment_id")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, paymentID)
	}
	if req.Amount.Valid {
		createStrings = append(createStrings, "amount")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		createStrings = append(createStrings, "payment_status")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		createStrings = append(createStrings, "payment_type")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		createStrings = append(createStrings, "order_id")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		createStrings = append(createStrings, "paramx")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		createStrings = append(createStrings, "auth_no")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		createStrings = append(createStrings, "confirmation_key")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		createStrings = append(createStrings, "success")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Success.String)
	}
	if req.PelecardToken.Valid {
		createStrings = append(createStrings, "pelecard_token")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		createStrings = append(createStrings, "transaction_id")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		createStrings = append(createStrings, "error_msg")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		createStrings = append(createStrings, "cardhebrew_name")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		createStrings = append(createStrings, "cc_abroad_card")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		createStrings = append(createStrings, "cc_brand")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		createStrings = append(createStrings, "cc_company_clearer")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		createStrings = append(createStrings, "cc_company_issuer")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		createStrings = append(createStrings, "credit_type")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		createStrings = append(createStrings, "cc_exp_date")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		createStrings = append(createStrings, "cc_number")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		createStrings = append(createStrings, "debit_code")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		createStrings = append(createStrings, "debit_currency")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		createStrings = append(createStrings, "debit_total")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		createStrings = append(createStrings, "debit_type")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		createStrings = append(createStrings, "first_payment_total")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		createStrings = append(createStrings, "fixed_payment_total")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		createStrings = append(createStrings, "j_param")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.JParam.String)
	}
	if req.TotalPayments.Valid {
		createStrings = append(createStrings, "total_payments")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		createStrings = append(createStrings, "transaction_init_time")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		createStrings = append(createStrings, "transaction_update_time")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		createStrings = append(createStrings, "voucher_id")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		createStrings = append(createStrings, "ord_key")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Ordkey.String)
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

func preparePelecardPaymentUpdateQuery(req PaymentUpdate) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.Amount.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("amount=$%d", len(updateStrings)+1))
		args = append(args, req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("payment_status=$%d", len(updateStrings)+1))
		args = append(args, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("payment_type=$%d", len(updateStrings)+1))
		args = append(args, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("order_id=$%d", len(updateStrings)+1))
		args = append(args, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("paramx=$%d", len(updateStrings)+1))
		args = append(args, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("auth_no=$%d", len(updateStrings)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("confirmation_key=$%d", len(updateStrings)+1))
		args = append(args, req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("success=$%d", len(updateStrings)+1))
		args = append(args, req.Success.String)
	}
	if req.PelecardToken.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("pelecard_token=$%d", len(updateStrings)+1))
		args = append(args, req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_id=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("error_msg=$%d", len(updateStrings)+1))
		args = append(args, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cardhebrew_name=$%d", len(updateStrings)+1))
		args = append(args, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_abroad_card=$%d", len(updateStrings)+1))
		args = append(args, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_brand=$%d", len(updateStrings)+1))
		args = append(args, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_company_clearer=$%d", len(updateStrings)+1))
		args = append(args, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_company_issuer=$%d", len(updateStrings)+1))
		args = append(args, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("credit_type=$%d", len(updateStrings)+1))
		args = append(args, req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_exp_date=$%d", len(updateStrings)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_number=$%d", len(updateStrings)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_code=$%d", len(updateStrings)+1))
		args = append(args, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_currency=$%d", len(updateStrings)+1))
		args = append(args, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_total=$%d", len(updateStrings)+1))
		args = append(args, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_type=$%d", len(updateStrings)+1))
		args = append(args, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("first_payment_total=$%d", len(updateStrings)+1))
		args = append(args, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("fixed_payment_total=$%d", len(updateStrings)+1))
		args = append(args, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("j_param=$%d", len(updateStrings)+1))
		args = append(args, req.JParam.String)
	}
	if req.TotalPayments.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("total_payments=$%d", len(updateStrings)+1))
		args = append(args, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_init_time=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_update_time=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("voucher_id=$%d", len(updateStrings)+1))
		args = append(args, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("ord_key=$%d", len(updateStrings)+1))
		args = append(args, req.Ordkey.String)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func preparePelecardPaymentUpdateViaPaymentStructQuery(req Payment) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.Amount.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("amount=$%d", len(updateStrings)+1))
		args = append(args, req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("payment_status=$%d", len(updateStrings)+1))
		args = append(args, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("payment_type=$%d", len(updateStrings)+1))
		args = append(args, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("order_id=$%d", len(updateStrings)+1))
		args = append(args, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("paramx=$%d", len(updateStrings)+1))
		args = append(args, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("auth_no=$%d", len(updateStrings)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("confirmation_key=$%d", len(updateStrings)+1))
		args = append(args, req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("success=$%d", len(updateStrings)+1))
		args = append(args, req.Success.String)
	}
	if req.PelecardToken.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("pelecard_token=$%d", len(updateStrings)+1))
		args = append(args, req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_id=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("error_msg=$%d", len(updateStrings)+1))
		args = append(args, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cardhebrew_name=$%d", len(updateStrings)+1))
		args = append(args, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_abroad_card=$%d", len(updateStrings)+1))
		args = append(args, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_brand=$%d", len(updateStrings)+1))
		args = append(args, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_company_clearer=$%d", len(updateStrings)+1))
		args = append(args, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_company_issuer=$%d", len(updateStrings)+1))
		args = append(args, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("credit_type=$%d", len(updateStrings)+1))
		args = append(args, req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_exp_date=$%d", len(updateStrings)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_number=$%d", len(updateStrings)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_code=$%d", len(updateStrings)+1))
		args = append(args, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_currency=$%d", len(updateStrings)+1))
		args = append(args, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_total=$%d", len(updateStrings)+1))
		args = append(args, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("debit_type=$%d", len(updateStrings)+1))
		args = append(args, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("first_payment_total=$%d", len(updateStrings)+1))
		args = append(args, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("fixed_payment_total=$%d", len(updateStrings)+1))
		args = append(args, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("j_param=$%d", len(updateStrings)+1))
		args = append(args, req.JParam.String)
	}
	if req.TotalPayments.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("total_payments=$%d", len(updateStrings)+1))
		args = append(args, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_init_time=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("transaction_update_time=$%d", len(updateStrings)+1))
		args = append(args, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("voucher_id=$%d", len(updateStrings)+1))
		args = append(args, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("ord_key=$%d", len(updateStrings)+1))
		args = append(args, req.Ordkey.String)
	}
	if req.Terminal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("terminal=$%d", len(updateStrings)+1))
		args = append(args, req.Terminal.String)
	}
	// pricing_version and pricing_evaluation live on the payments table only, not payments_pelecard.

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func prepareOfflinePaymentCreateQuery(req RequestOrder, paymentID int, status string) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.PaymentMethod.Valid {
		createStrings = append(createStrings, `payment_method`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentMethod.String)
	}
	if paymentID != 0 {
		createStrings = append(createStrings, `payment_id`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, paymentID)
	}
	if req.Receipt.Valid {
		createStrings = append(createStrings, `receipt`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Receipt.String)
	}
	if req.ExtraInfo.Valid {
		createStrings = append(createStrings, `extra_info`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ExtraInfo.String)
	}
	if req.Properties.Valid {
		createStrings = append(createStrings, `properties`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, string(req.Properties.JSON))
	}
	if status != "" {
		createStrings = append(createStrings, `status`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, status)
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}

func prepareOfflinePaymentUpdateQuery(req PaymentUpdate) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.PaymentMethod.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`payment_method=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentMethod.String)
	}
	if req.Receipt.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`receipt=$%d`, len(updateStrings)+1))
		args = append(args, req.Receipt.String)
	}
	if req.ExtraInfo.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`extra_info=$%d`, len(updateStrings)+1))
		args = append(args, req.ExtraInfo.String)
	}
	if req.Properties.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`properties=$%d`, len(updateStrings)+1))
		args = append(args, string(req.Properties.JSON))
	}
	if req.Status.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`status=$%d`, len(updateStrings)+1))
		args = append(args, req.Status.String)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func prepareHelpHaverPaymentCreateQuery(req RequestOrder, paymentID int, status string) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if status != "" {
		createStrings = append(createStrings, `status`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, status)
	}
	if paymentID != 0 {
		createStrings = append(createStrings, `payment_id`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, paymentID)
	}
	if req.ValidationMessage.Valid {
		createStrings = append(createStrings, `validation_message`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Receipt.String)
	}
	if req.RejectionMessage.Valid {
		createStrings = append(createStrings, `rejection_message`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ExtraInfo.String)
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}

func prepareHelpHaverPaymentUpdateQuery(req PaymentUpdate) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.Status.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`status=$%d`, len(updateStrings)+1))
		args = append(args, req.Status.String)
	}
	if req.ValidationMessage.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`validation_message=$%d`, len(updateStrings)+1))
		args = append(args, req.ValidationMessage.String)
	}
	if req.RejectionMessage.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`rejection_message=$%d`, len(updateStrings)+1))
		args = append(args, req.RejectionMessage.String)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func buildAndGetWherePaymentActQuery(email string, productType string, paymentType string) (string, string) {

	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	whereCondition.WriteString(` p."OrderID" = o.id AND o."AccountID" = a.id`)

	// WHERE query generation based on parameters
	if email != "" {
		whereCondition.WriteString(fmt.Sprintf(` AND LOWER(a."Email") LIKE LOWER('%%%s%%')`, email))
	}

	if productType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND LOWER(o."ProductType")=LOWER('%s')`, productType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` LOWER(o."ProductType")=LOWER('%s')`, productType))
		}
	}

	if paymentType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND LOWER(p."PaymentType")=LOWER('%s')`, paymentType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` LOWER(p."PaymentType")=LOWER('%s')`, paymentType))
		}
	}

	orderBy.WriteString(" order by p.created_at desc")

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String()
}

func buildAndGetPaymentsWhereQuery(fromDate string, dateTo *time.Time, paymentType string, paymentStatus string,
	orderType string, email string, accontID int, paymentsWithToken string, intOrderID int, orderByCreatedAt string) (string, string, error) {
	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	if !dateTo.IsZero() {
		whereCondition.WriteString(fmt.Sprintf(" p.updated_at <= '%s'", dateTo.Format("2006-01-02 15:04:05")))
	}

	// WHERE query generation based on parameters
	if fromDate != "" {
		rfcLayout := time.RFC3339
		fromDateParsed, err := time.Parse(rfcLayout, fromDate)

		if err != nil {
			return "", "", err
		}
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(" AND p.updated_at >= '%s'", fromDateParsed.Format("2006-01-02 15:04:05")))
		} else {
			whereCondition.WriteString(fmt.Sprintf(" p.updated_at >= '%s'", fromDateParsed.Format("2006-01-02 15:04:05")))
		}
	}

	if paymentType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND LOWER(p."PaymentType")=LOWER('%s')`, paymentType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` LOWER(p."PaymentType")=LOWER('%s')`, paymentType))
		}
	}

	if paymentStatus != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND LOWER(p."PaymentStatus")=LOWER('%s')`, paymentStatus))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` LOWER(p."PaymentStatus")=LOWER('%s')`, paymentStatus))
		}
	}

	if paymentsWithToken != "" {
		//convert string to bool
		paymentsWithTokenBool, err := strconv.ParseBool(paymentsWithToken)
		if err != nil {
			return "", "", err
		}
		if paymentsWithTokenBool {
			if whereCondition.String() != "" {
				whereCondition.WriteString(" AND p.pelecard_token != ''")
			} else {
				whereCondition.WriteString(" p.pelecard_token != ''")
			}
		} else {
			if whereCondition.String() != "" {
				whereCondition.WriteString(" AND p.pelecard_token = ''")
			} else {
				whereCondition.WriteString(" p.pelecard_token = ''")
			}

		}

	}

	if orderType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND o.id = p."OrderID" AND LOWER(o."Type")=LOWER('%s')`, orderType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` o.id = p."OrderID" AND LOWER(o."Type")=LOWER('%s')`, orderType))
		}

	}

	if email != "" || accontID != 0 {
		if email != "" {
			if whereCondition.String() != "" {
				whereCondition.WriteString(fmt.Sprintf(` AND p."OrderID" = o.id AND a.id = o."AccountID" AND LOWER(a."Email")=LOWER('%s')`, email))
			} else {
				whereCondition.WriteString(fmt.Sprintf(` p."OrderID" = o.id AND a.id = o."AccountID" AND LOWER(a."Email")=LOWER('%s')`, email))
			}
		} else {
			if whereCondition.String() != "" {
				whereCondition.WriteString(fmt.Sprintf(` AND p."OrderID" = o.id AND a.id = o."AccountID" AND a.id=%d`, accontID))
			} else {
				whereCondition.WriteString(fmt.Sprintf(` p."OrderID" = o.id AND a.id = o."AccountID" AND a.id=%d`, accontID))
			}
		}
	}

	if intOrderID != 0 {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND p."OrderID" = %d`, intOrderID))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` p."OrderID" = %d`, intOrderID))
		}
	}

	if orderType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND o.id = p."OrderID" AND LOWER(o."Type")=LOWER('%s')`, orderType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` o.id = p."OrderID" AND LOWER(o."Type")=LOWER('%s')`, orderType))
		}
	}

	if orderByCreatedAt != "" {
		if strings.ToLower(orderByCreatedAt) != "desc" && strings.ToLower(orderByCreatedAt) != "asc" {
			orderByCreatedAt = "asc"
		}
		orderBy.WriteString(fmt.Sprintf(" ORDER BY p.created_at %s", orderByCreatedAt))
	} else {
		orderBy.WriteString(fmt.Sprintf(" ORDER BY p.updated_at %s", "desc"))
	}

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}

func buildAndGetOfflinePaymentsWhereQuery(method string, orderByCreatedAt string) (string, string, error) {
	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	// WHERE query generation based on parameters
	if method != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(` AND p.payment_method='%s')`, method))
		} else {
			whereCondition.WriteString(fmt.Sprintf(` p.payment_method='%s'`, method))
		}
	}

	if orderByCreatedAt != "" {
		if strings.ToLower(orderByCreatedAt) != "desc" && strings.ToLower(orderByCreatedAt) != "asc" {
			orderByCreatedAt = "asc"
		}
		orderBy.WriteString(fmt.Sprintf(" ORDER BY p.created_at %s", orderByCreatedAt))
	} else {
		orderBy.WriteString(fmt.Sprintf(" ORDER BY p.updated_at %s", "desc"))
	}

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}

func preparePaymentCreateQuery(req Payment) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.Amount.Valid {
		createStrings = append(createStrings, `"Amount"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Amount.Float64)
	}
	if req.Currency.Valid {
		createStrings = append(createStrings, `"Currency"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Currency.String)
	}
	if req.PaymentStatus.Valid {
		createStrings = append(createStrings, `"PaymentStatus"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		createStrings = append(createStrings, `"PaymentType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		createStrings = append(createStrings, `"OrderID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		createStrings = append(createStrings, `"ParamX"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		createStrings = append(createStrings, `"AuthNo"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		createStrings = append(createStrings, "confirmation_key")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		createStrings = append(createStrings, "success")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Success.String)
	}
	if req.PelecardToken.Valid {
		createStrings = append(createStrings, "pelecard_token")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		createStrings = append(createStrings, `"TransactionID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		createStrings = append(createStrings, `"ErrorMsg"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		createStrings = append(createStrings, `"CardHebrewName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		createStrings = append(createStrings, `"CCAbroadCard"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		createStrings = append(createStrings, `"CCBrand"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		createStrings = append(createStrings, `"CCCompanyClearer"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		createStrings = append(createStrings, `"CCCompanyIssuer"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		createStrings = append(createStrings, "credit_type")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		createStrings = append(createStrings, `"CCExpDate"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		createStrings = append(createStrings, `"CCNumber"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		createStrings = append(createStrings, `"DebitCode"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		createStrings = append(createStrings, `"DebitCurrency"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		createStrings = append(createStrings, `"DebitTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		createStrings = append(createStrings, `"DebitType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		createStrings = append(createStrings, `"FirstPaymentTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		createStrings = append(createStrings, `"FixedPaymentTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		createStrings = append(createStrings, "j_param")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.JParam.String)
	}
	if req.TotalPayments.Valid {
		createStrings = append(createStrings, `"TotalPayments"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		createStrings = append(createStrings, `"TransactionInitTime"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		createStrings = append(createStrings, `"TransactionUpdateTime"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		createStrings = append(createStrings, `"VoucherID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		createStrings = append(createStrings, `"Ordkey"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Ordkey.String)
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

func preparePaymentUpdateQuery(req Payment) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.Amount.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Amount"=$%d`, len(updateStrings)+1))
		args = append(args, req.Amount.Float64)
	}
	if req.Currency.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Currency"=$%d`, len(updateStrings)+1))
		args = append(args, req.Currency.String)
	}
	if req.PaymentStatus.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"PaymentStatus"=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"PaymentType"=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"OrderID"=$%d`, len(updateStrings)+1))
		args = append(args, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"ParamX"=$%d`, len(updateStrings)+1))
		args = append(args, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"AuthNo"=$%d`, len(updateStrings)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("confirmation_key=$%d", len(updateStrings)+1))
		args = append(args, req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("success=$%d", len(updateStrings)+1))
		args = append(args, req.Success.String)
	}
	if req.PelecardToken.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("pelecard_token=$%d", len(updateStrings)+1))
		args = append(args, req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionID"=$%d`, len(updateStrings)+1))
		args = append(args, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"ErrorMsg"=$%d`, len(updateStrings)+1))
		args = append(args, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CardHebrewName"=$%d`, len(updateStrings)+1))
		args = append(args, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCAbroadCard"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCBrand"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCCompanyClearer"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCCompanyIssuer"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("credit_type=$%d", len(updateStrings)+1))
		args = append(args, req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCExpDate"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCNumber"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitCode"=$%d`, len(updateStrings)+1))
		args = append(args, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitCurrency"=$%d`, len(updateStrings)+1))
		args = append(args, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitTotal"=$%d`, len(updateStrings)+1))
		args = append(args, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitType"=$%d`, len(updateStrings)+1))
		args = append(args, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"FirstPaymentTotal"=$%d`, len(updateStrings)+1))
		args = append(args, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"FixedPaymentTotal"=$%d`, len(updateStrings)+1))
		args = append(args, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("j_param=$%d", len(updateStrings)+1))
		args = append(args, req.JParam.String)
	}
	if req.TotalPayments.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TotalPayments"=$%d`, len(updateStrings)+1))
		args = append(args, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionInitTime"=$%d`, len(updateStrings)+1))
		args = append(args, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionUpdateTime"=$%d`, len(updateStrings)+1))
		args = append(args, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"VoucherID"=$%d`, len(updateStrings)+1))
		args = append(args, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Ordkey"=$%d`, len(updateStrings)+1))
		args = append(args, req.Ordkey.String)
	}
	if req.PricingVersion.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("pricing_version=$%d", len(updateStrings)+1))
		args = append(args, req.PricingVersion.String)
	}
	if req.PricingEvaluation.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("pricing_evaluation=$%d", len(updateStrings)+1))
		args = append(args, req.PricingEvaluation.JSON)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func (o *OrdersDB) FetchPaymentByParamX(ctx context.Context, paramX string) (*PaymentWithFullName, error) {
	var p PaymentWithFullName

	if err := o.QueryRow(ctx, `select a."UserKey", a.id, a."FirstName", a."LastName", a."Email", a."Street", a."City", o."OrderLanguage", o."Amount", o."Currency", 
	p.id, p."Amount", p."PaymentStatus", p."PaymentType", p."OrderID", p."ParamX", p."AuthNo", p.confirmation_key,
	p.success, p.pelecard_token, p."TransactionID", p."ErrorMsg", p."CardHebrewName", p."CCAbroadCard", p."CCBrand",
	p."CCCompanyClearer", p."CCCompanyIssuer", p.credit_type, p."CCExpDate", p."CCNumber", p."DebitCode", p."DebitCurrency",
	p."DebitTotal", p."DebitType", p."FirstPaymentTotal", p."FixedPaymentTotal", p.j_param, p."TotalPayments",
	p."TransactionInitTime", p."TransactionUpdateTime", p."VoucherID", p."Ordkey", p.created_at, p.updated_at, p.deleted_at, o."SKU" 
	from accounts as a, orders as o, payments as p
	where p."ParamX" = $1
	and p."OrderID" = o.id 
	and a.id = o."AccountID"
	order by p."ParamX" asc`, paramX).Scan(
		&p.UserKey, &p.AccountID, &p.FirstName, &p.LastName, &p.Email, &p.Street, &p.City, &p.Language, &p.Amount, &p.Currency,
		&p.ID, &p.Amount, &p.PaymentStatus, &p.PaymentType, &p.OrderID, &p.ParamX, &p.AuthNo, &p.ConfirmationKey,
		&p.Success, &p.PelecardToken, &p.TransactionID, &p.ErrorMsg, &p.CardHebrewName, &p.CCAbroadCard, &p.CCBrand,
		&p.CCCompanyClearer, &p.CCCompanyIssuer, &p.CreditType, &p.CCExpDate, &p.CCNumber, &p.DebitCode,
		&p.DebitCurrency, &p.DebitTotal, &p.DebitType, &p.FirstPaymentTotal, &p.FixedPaymentTotal, &p.JParam,
		&p.TotalPayments, &p.TransactionInitTime, &p.TransactionUpdateTime, &p.VoucherID,
		&p.Ordkey, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.SKU,
	); err != nil {
		return nil, err
	}

	return &p, nil
}
