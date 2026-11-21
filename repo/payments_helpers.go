package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
)

func (o *OrdersDB) GetPaymentByID(ctx context.Context, id int) (*Payment, error) {
	var (
		pay      Payment
		addQuery string
	)
	if err := o.pool.QueryRow(ctx, `SELECT
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
	_, err := o.pool.Exec(ctx, "UPDATE payments SET deleted_at = $1 WHERE id = $2", time.Now(), paymentID)
	if err != nil {
		return err
	}
	o.emitEvent(ctx, events.TypeDeletePayment, map[string]interface{}{"payment_id": paymentID})
	return nil
}

// GetPaymentActivities lists payments with their order and account. A
// non-empty userKey limits them to the accounts with that keycloak id (a user
// can have more than one); email is a substring search. A non-admin is pinned
// by userKey, never by email: they can edit their account's email, so it
// cannot be what keeps them out of others' data.
func (o *OrdersDB) GetPaymentActivities(ctx context.Context, userKey string, email string, productType string, paymentType string, skip int, limit int) ([]PaymentActivitiesRes, error) {
	userDbWhereQuery, orderByQuery, args := buildAndGetWherePaymentActQuery(userKey, email, productType, paymentType)

	// Built first: args.arg appends to args, and Go does not order that
	// against reading args for the variadic argument of the same call.
	query := `SELECT p.created_at,  p."Amount", p."PaymentType",  p."OrderID", 
	p."ParamX", p."PaymentStatus", p."CCNumber", p."CCExpDate", 
	o."ProductType", o."Type", o."Currency",
	a."FirstName", a."LastName", a."Email", a."Country" 
	from payments as p, orders as o, accounts as a` +
		userDbWhereQuery + orderByQuery + " LIMIT " + args.arg(limit) + " OFFSET " + args.arg(skip)
	rows, err := o.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Query: %w", err)
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

// GetAllPayments lists payments by the given filters. A non-empty userKey
// limits them to the accounts with that keycloak id (a user can have more
// than one); it is what pins a non-admin, and email and accountID only narrow
// within it.
func (o *OrdersDB) GetAllPayments(ctx context.Context, skip int, limit int, fromDate *time.Time, toDate *time.Time,
	paymentType string, paymentStatus string, orderType string, email string, accountID int, userKey string, paymentsWithToken *bool,
	intOrderID int, orderByCreatedAt string) ([]Payment, error) {

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery, args := buildAndGetPaymentsWhereQuery(fromDate, toDate, paymentType, paymentStatus,
		orderType, email, accountID, userKey, paymentsWithToken, intOrderID, orderByCreatedAt)

	fromQuery := " FROM payments as p"
	if email != "" || accountID != 0 || userKey != "" || orderType != "" {
		fromQuery = fromQuery + ", orders as o"
		if email != "" || accountID != 0 || userKey != "" {
			fromQuery = fromQuery + ", accounts as a"
		}
	}

	rows, err := o.pool.Query(ctx, `SELECT 
	p.id, p.created_at, p.updated_at, p.deleted_at, p."Amount", p."Currency", p."PaymentStatus", p."PaymentType", 
	p."OrderID", p."ParamX", p."Ordkey", p."AuthNo", p.confirmation_key, p.success, p.pelecard_token, p."TransactionID", 
	p."ErrorMsg", p."CardHebrewName", p."CCAbroadCard", p."CCBrand", p."CCCompanyClearer", p."CCCompanyIssuer", 
	p.credit_type, p."CCExpDate", p."CCNumber", p."DebitCode", p."DebitCurrency", p."DebitTotal", p."DebitType", 
	p."FirstPaymentTotal", p."FixedPaymentTotal", p."TotalPayments", p.j_param, p."TransactionInitTime", 
	p."TransactionUpdateTime", p."VoucherID"`+fromQuery+whereQuery+orderByQuery+limitOffsetString, args...)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Query: %w", err)
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

// GetTotalParticipationStatusCount counts what GetPaymentActivities lists;
// userKey means the same there.
func (o *OrdersDB) GetTotalParticipationStatusCount(ctx context.Context, userKey string, email string, productType string,
	paymentType string) (int, error) {
	var count int

	userDbWhereQuery, _, args := buildAndGetWherePaymentActQuery(userKey, email, productType, paymentType)
	err := o.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments as p, orders as o, accounts as a`+userDbWhereQuery, args...).
		Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (o *OrdersDB) GetPaymentByEmail(ctx context.Context, email string) ([]PaymentByEmail, error) {
	rows, err := o.pool.Query(ctx, `select p."OrderID", p.created_at, o."PaymentDate", o."Type", o."ProductType", p."Amount", 
	p."Currency", p."CCNumber", p."ParamX", p."PaymentStatus"
	from payments as p, orders as o, accounts as a
	where a."Email" = $1
	and a.id = o."AccountID"
	and o.id = p."OrderID"
	order by p.created_at desc`, email)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Query: %w", err)
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
	whereQuery, orderByQuery, args, err := buildAndGetOfflinePaymentsWhereQuery(method, orderByCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("buildAndGetOfflinePaymentsWhereQuery: %w", err)
	}

	rows, err := o.pool.Query(ctx, `SELECT 
	p.id, p.created_at, p.updated_at, p.deleted_at, p.payment_method, p.receipt, p.extra_info, p.status, p.payment_id,
	p.properties`+fromQuery+whereQuery+orderByQuery+limitOffsetString, args...)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Query: %w", err)
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
		Amount:            req.Amount,
		Currency:          req.Currency,
		PaymentType:       null.NewString(paymentType, true),
		OrderID:           null.NewInt(orderID, true),
		PaymentStatus:     null.NewString(paymentStatus, true),
		PricingVersion:    req.PricingVersion,
		PricingEvaluation: req.PricingEvaluation,
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)

	if err := o.pool.QueryRow(ctx, `INSERT INTO payments (`+createString+`) VALUES (`+numString+`) RETURNING id`,
		createQueryArgs...).Scan(&p.ID); err != nil {
		return nil, fmt.Errorf("o.pool.QueryRow.Scan: %w", err)
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
	// Ids are int4: parsing within 32 bits makes the int conversion exact.
	parsedOrder, err := strconv.ParseInt(strings.Split(req.UserKey.String, "-")[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("strconv.ParseInt [user_key]: %w", err)
	}
	parsedPayment, err := strconv.ParseInt(strings.Split(req.ParamX.String, "-")[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("strconv.ParseInt [additional_details_param_x]: %w", err)
	}
	orderid, paymentid := int(parsedOrder), int(parsedPayment)

	// One transaction, the payment row locked: the status check, the payments
	// write and the payments_pelecard write happen together or not at all. A
	// successful payment is then written once — a notify and a browser return
	// racing each other cannot both write, and a late success=0 cannot flip it
	// to failed — and a failure between the two writes leaves neither, so a
	// retry writes both.
	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var p Payment
	// Amount and Ordkey are read for writePelecardFacts, to create a missing
	// payments_pelecard row. They stay out of p: the update builders write every
	// set field, so carrying them would rewrite payments and copy them over a
	// payments_pelecard row someone corrected by hand.
	var amount null.Float64
	var ordkey null.String
	if err := tx.QueryRow(ctx, `SELECT
	"OrderID",
	"Amount",
	"Ordkey",
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
	"VoucherID" FROM payments WHERE "OrderID"=$1 AND id=$2 FOR UPDATE`, orderid, paymentid).Scan(
		&p.OrderID, &amount, &ordkey, &p.PaymentStatus, &p.PaymentType, &p.ParamX, &p.AuthNo, &p.ConfirmationKey, &p.Success,
		&p.PelecardToken, &p.TransactionID, &p.CCBrand, &p.CardHebrewName, &p.CCAbroadCard,
		&p.CCCompanyClearer, &p.CreditType, &p.CCExpDate, &p.CCNumber, &p.DebitCode, &p.DebitCurrency,
		&p.DebitTotal, &p.DebitType, &p.FirstPaymentTotal, &p.FixedPaymentTotal, &p.TotalPayments,
		&p.JParam, &p.TransactionInitTime, &p.TransactionUpdateTime, &p.VoucherID,
	); err != nil {
		return nil, fmt.Errorf("tx.QueryRow.Scan [payment]: %w", err)
	}
	if p.PaymentStatus.String == common.PaymentStatusSuccess {
		return nil, common.ErrAlreadyPaid
	}
	// Checked after the status, under the lock: a return carrying an error for
	// a payment already confirmed is answered as already paid, and only one
	// for a payment still pending is refused.
	if !req.Error.IsZero() {
		return nil, fmt.Errorf("req.Error: %s", req.Error.String)
	}

	if req.Success.String == "1" {
		// A browser return can send a field explicitly empty, which would be
		// stored as ''; checkout's notify sends NULL for it. Every field is
		// taken through blankAsNull so both paths store the same.
		p.PaymentStatus = null.NewString("success", true)
		p.PaymentType = null.StringFrom(common.PaymentTypePelecard)
		p.ParamX = req.ParamX
		p.AuthNo = blankAsNull(req.AuthNo)
		p.ConfirmationKey = blankAsNull(req.ConfirmationKey)
		p.Success = req.Success
		p.PelecardToken = blankAsNull(req.Token)
		p.TransactionID = blankAsNull(req.TransactionID)
		p.CCBrand = blankAsNull(req.CCBrand)
		p.CardHebrewName = blankAsNull(req.CardHebrewName)
		p.CCAbroadCard = blankAsNull(req.CCAbroadCard)
		p.CCCompanyClearer = blankAsNull(req.CCCompanyClearer)
		p.CCCompanyIssuer = blankAsNull(req.CCCompanyIssuer)
		p.CreditType = blankAsNull(req.CreditType)
		p.CCExpDate = blankAsNull(req.CCExpDate)
		p.CCNumber = blankAsNull(req.CCNumber)
		p.DebitCode = blankAsNull(req.DebitCode)
		p.DebitCurrency = blankAsNull(req.DebitCurrency)
		p.DebitTotal = blankAsNull(req.DebitTotal)
		p.DebitType = blankAsNull(req.DebitType)
		p.FirstPaymentTotal = blankAsNull(req.FirstPaymentTotal)
		p.FixedPaymentTotal = blankAsNull(req.FixedPaymentTotal)
		p.TotalPayments = blankAsNull(req.TotalPayments)
		p.JParam = blankAsNull(req.JParam)
		p.TransactionInitTime = blankAsNull(req.TransactionInitTime)
		p.TransactionUpdateTime = blankAsNull(req.TransactionUpdateTime)
		p.VoucherID = blankAsNull(req.VoucherID)
	} else {
		p.PaymentStatus = null.NewString("failed", true)
		p.ErrorMsg = null.NewString("Failed", true) // TODO: improve
		p.PaymentType = null.StringFrom(common.PaymentTypePelecard)
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)
	// The id is bound like every other value, not formatted into the SQL.
	// The row is held FOR UPDATE above, so this write finds it.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE payments SET %s WHERE id=$%d`, toUpdate, len(toUpdateArgs)+1),
		append(toUpdateArgs, paymentid)...); err != nil {
		return nil, fmt.Errorf("tx.Exec [update payment]: %w", err)
	}

	if err := writePelecardFacts(ctx, tx, p, amount, ordkey, paymentid); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("tx.Commit: %w", err)
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": paymentid})

	return &p, nil
}

// writePelecardFacts stores the gateway's facts in the payment's
// payments_pelecard row, inside the payment's transaction. Older payments
// (and a few recent ones) have no such row; one is created rather than
// committing a confirmed payment without its card facts.
func writePelecardFacts(ctx context.Context, tx pgx.Tx, p Payment, amount null.Float64, ordkey null.String, paymentID int) error {
	toUpdate, args := preparePelecardPaymentUpdateViaPaymentStructQuery(p)
	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=$%d`, toUpdate, len(args)+1),
		append(args, int64(paymentID))...)
	if err != nil {
		return fmt.Errorf("tx.Exec [update pelecard]: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	ins := p
	ins.Amount, ins.Ordkey = amount, ordkey
	columns, placeholders, createArgs := preparePelecardPaymentCreateQuery(ins, paymentID)
	if _, err := tx.Exec(ctx, `INSERT INTO payments_pelecard (`+columns+`) VALUES (`+placeholders+`)`,
		createArgs...); err != nil {
		return fmt.Errorf("tx.Exec [insert pelecard]: %w", err)
	}
	return nil
}

func (o *OrdersDB) createOfflinePayment(ctx context.Context, req RequestOrder, paymentID int, status string) error {
	createString, numString, createQueryArgs := prepareOfflinePaymentCreateQuery(req, paymentID, status)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.pool.Exec(ctx, `INSERT INTO payments_offline (`+createString+`) VALUES (`+numString+`)`, createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) createPelecardPayment(ctx context.Context, req RequestOrder, paymentID int, p Payment) error {
	createString, numString, createQueryArgs := preparePelecardPaymentCreateQuery(p, paymentID)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.pool.Exec(ctx, `INSERT INTO payments_pelecard (`+createString+`) VALUES (`+numString+`)`, createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) createHelpHaverPayment(ctx context.Context, req RequestOrder, paymentID int, status string) error {
	createString, numString, createQueryArgs := prepareHelpHaverPaymentCreateQuery(req, paymentID, status)
	if len(createQueryArgs) == 0 {
		return common.ErrInvalidValues
	}

	_, err := o.pool.Exec(ctx, `INSERT INTO payments_helphaver (`+createString+`) VALUES (`+numString+`)`, createQueryArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}

	return nil
}

func (o *OrdersDB) UpdatePelecardPayment(ctx context.Context, req PaymentUpdate) error {
	toUpdate, toUpdateArgs := preparePelecardPaymentUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.pool.Exec(ctx, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
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

	updateRes, err := o.pool.Exec(ctx, fmt.Sprintf(`UPDATE payments_offline SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
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

	updateRes, err := o.pool.Exec(ctx, fmt.Sprintf(`UPDATE payments_helphaver SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	o.emitEvent(ctx, events.TypeUpdatePayment, map[string]interface{}{"payment_id": req.PaymentID.Int})

	return nil
}

func (o *OrdersDB) UpdateParentPaymentTableStatusAndReturnOrderId(ctx context.Context, status string, paymentID int) (int, error) {
	var orderId int
	if err := o.pool.QueryRow(ctx, `UPDATE payments SET "PaymentStatus"=$1 WHERE id=$2 RETURNING "OrderID"`, status, paymentID).
		Scan(&orderId); err != nil {
		return 0, err
	}
	return orderId, nil
}

func preparePelecardPaymentCreateQuery(req Payment, paymentID int) (string, string, queryArgs) {
	var c colVals

	if paymentID != 0 {
		c.add("payment_id", paymentID)
	}
	if req.Amount.Valid {
		c.add("amount", req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		c.add("payment_status", req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		c.add("payment_type", req.PaymentType.String)
	}
	if req.OrderID.Valid {
		c.add("order_id", req.OrderID.Int)
	}
	if req.ParamX.Valid {
		c.add("paramx", req.ParamX.String)
	}
	if req.AuthNo.Valid {
		c.add("auth_no", req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		c.add("confirmation_key", req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		c.add("success", req.Success.String)
	}
	if req.PelecardToken.Valid && req.PelecardToken.String != "" {
		c.add("pelecard_token", req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		c.add("transaction_id", req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		c.add("error_msg", req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		c.add("cardhebrew_name", req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		c.add("cc_abroad_card", req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		c.add("cc_brand", req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		c.add("cc_company_clearer", req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		c.add("cc_company_issuer", req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		c.add("credit_type", req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		c.add("cc_exp_date", req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		c.add("cc_number", req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		c.add("debit_code", req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		c.add("debit_currency", req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		c.add("debit_total", req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		c.add("debit_type", req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		c.add("first_payment_total", req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		c.add("fixed_payment_total", req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		c.add("j_param", req.JParam.String)
	}
	if req.TotalPayments.Valid {
		c.add("total_payments", req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		c.add("transaction_init_time", req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		c.add("transaction_update_time", req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		c.add("voucher_id", req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		c.add("ord_key", req.Ordkey.String)
	}
	if !c.empty() {
		c.add("created_at", time.Now())
		c.add("updated_at", time.Now())
	}

	return c.insertLists()
}

func preparePelecardPaymentUpdateQuery(req PaymentUpdate) (string, queryArgs) {
	var c colVals

	if req.Amount.Valid {
		c.add("amount", req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		c.add("payment_status", req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		c.add("payment_type", req.PaymentType.String)
	}
	if req.OrderID.Valid {
		c.add("order_id", req.OrderID.Int)
	}
	if req.ParamX.Valid {
		c.add("paramx", req.ParamX.String)
	}
	if req.AuthNo.Valid {
		c.add("auth_no", req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		c.add("confirmation_key", req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		c.add("success", req.Success.String)
	}
	if req.PelecardToken.Valid && req.PelecardToken.String != "" {
		c.add("pelecard_token", req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		c.add("transaction_id", req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		c.add("error_msg", req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		c.add("cardhebrew_name", req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		c.add("cc_abroad_card", req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		c.add("cc_brand", req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		c.add("cc_company_clearer", req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		c.add("cc_company_issuer", req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		c.add("credit_type", req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		c.add("cc_exp_date", req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		c.add("cc_number", req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		c.add("debit_code", req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		c.add("debit_currency", req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		c.add("debit_total", req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		c.add("debit_type", req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		c.add("first_payment_total", req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		c.add("fixed_payment_total", req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		c.add("j_param", req.JParam.String)
	}
	if req.TotalPayments.Valid {
		c.add("total_payments", req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		c.add("transaction_init_time", req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		c.add("transaction_update_time", req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		c.add("voucher_id", req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		c.add("ord_key", req.Ordkey.String)
	}

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func preparePelecardPaymentUpdateViaPaymentStructQuery(req Payment) (string, queryArgs) {
	var c colVals

	if req.Amount.Valid {
		c.add("amount", req.Amount.Float64)
	}
	if req.PaymentStatus.Valid {
		c.add("payment_status", req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		c.add("payment_type", req.PaymentType.String)
	}
	if req.OrderID.Valid {
		c.add("order_id", req.OrderID.Int)
	}
	if req.ParamX.Valid {
		c.add("paramx", req.ParamX.String)
	}
	if req.AuthNo.Valid {
		c.add("auth_no", req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		c.add("confirmation_key", req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		c.add("success", req.Success.String)
	}
	if req.PelecardToken.Valid && req.PelecardToken.String != "" {
		c.add("pelecard_token", req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		c.add("transaction_id", req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		c.add("error_msg", req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		c.add("cardhebrew_name", req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		c.add("cc_abroad_card", req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		c.add("cc_brand", req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		c.add("cc_company_clearer", req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		c.add("cc_company_issuer", req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		c.add("credit_type", req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		c.add("cc_exp_date", req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		c.add("cc_number", req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		c.add("debit_code", req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		c.add("debit_currency", req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		c.add("debit_total", req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		c.add("debit_type", req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		c.add("first_payment_total", req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		c.add("fixed_payment_total", req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		c.add("j_param", req.JParam.String)
	}
	if req.TotalPayments.Valid {
		c.add("total_payments", req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		c.add("transaction_init_time", req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		c.add("transaction_update_time", req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		c.add("voucher_id", req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		c.add("ord_key", req.Ordkey.String)
	}
	if req.Terminal.Valid {
		c.add("terminal", req.Terminal.String)
	}
	// pricing_version and pricing_evaluation live on the payments table only, not payments_pelecard.

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func prepareOfflinePaymentCreateQuery(req RequestOrder, paymentID int, status string) (string, string, queryArgs) {
	var c colVals

	if req.PaymentMethod.Valid {
		c.add(`payment_method`, req.PaymentMethod.String)
	}
	if paymentID != 0 {
		c.add(`payment_id`, paymentID)
	}
	if req.Receipt.Valid {
		c.add(`receipt`, req.Receipt.String)
	}
	if req.ExtraInfo.Valid {
		c.add(`extra_info`, req.ExtraInfo.String)
	}
	if req.Properties.Valid {
		c.add(`properties`, string(req.Properties.JSON))
	}
	if status != "" {
		c.add(`status`, status)
	}

	return c.insertLists()
}

func prepareOfflinePaymentUpdateQuery(req PaymentUpdate) (string, queryArgs) {
	var c colVals

	if req.PaymentMethod.Valid {
		c.add(`payment_method`, req.PaymentMethod.String)
	}
	if req.Receipt.Valid {
		c.add(`receipt`, req.Receipt.String)
	}
	if req.ExtraInfo.Valid {
		c.add(`extra_info`, req.ExtraInfo.String)
	}
	if req.Properties.Valid {
		c.add(`properties`, string(req.Properties.JSON))
	}
	if req.Status.Valid {
		c.add(`status`, req.Status.String)
	}

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func prepareHelpHaverPaymentCreateQuery(req RequestOrder, paymentID int, status string) (string, string, queryArgs) {
	var c colVals

	if status != "" {
		c.add(`status`, status)
	}
	if paymentID != 0 {
		c.add(`payment_id`, paymentID)
	}
	if req.ValidationMessage.Valid {
		c.add(`validation_message`, req.Receipt.String)
	}
	if req.RejectionMessage.Valid {
		c.add(`rejection_message`, req.ExtraInfo.String)
	}

	return c.insertLists()
}

func prepareHelpHaverPaymentUpdateQuery(req PaymentUpdate) (string, queryArgs) {
	var c colVals

	if req.Status.Valid {
		c.add(`status`, req.Status.String)
	}
	if req.ValidationMessage.Valid {
		c.add(`validation_message`, req.ValidationMessage.String)
	}
	if req.RejectionMessage.Valid {
		c.add(`rejection_message`, req.RejectionMessage.String)
	}

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func buildAndGetWherePaymentActQuery(userKey string, email string, productType string, paymentType string) (string, string, queryArgs) {

	var args queryArgs
	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	whereCondition.WriteString(` p."OrderID" = o.id AND o."AccountID" = a.id`)

	// WHERE query generation based on parameters
	if userKey != "" {
		whereCondition.WriteString(` AND a."UserKey" = ` + args.arg(userKey))
	}

	if email != "" {
		whereCondition.WriteString(` AND LOWER(a."Email") LIKE LOWER('%' || ` + args.arg(email) + ` || '%')`)
	}

	if productType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND LOWER(o."ProductType")=LOWER(` + args.arg(productType) + `)`)
		} else {
			whereCondition.WriteString(` LOWER(o."ProductType")=LOWER(` + args.arg(productType) + `)`)
		}
	}

	if paymentType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND LOWER(p."PaymentType")=LOWER(` + args.arg(paymentType) + `)`)
		} else {
			whereCondition.WriteString(` LOWER(p."PaymentType")=LOWER(` + args.arg(paymentType) + `)`)
		}
	}

	orderBy.WriteString(" order by p.created_at desc")

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), args
}

func buildAndGetPaymentsWhereQuery(fromDate *time.Time, dateTo *time.Time, paymentType string, paymentStatus string,
	orderType string, email string, accontID int, userKey string, paymentsWithToken *bool, intOrderID int, orderByCreatedAt string) (string, string, []any) {
	var args queryArgs
	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	if !dateTo.IsZero() {
		whereCondition.WriteString(fmt.Sprintf(" p.updated_at <= '%s'", dateTo.Format(time.RFC3339Nano)))
	}

	// WHERE query generation based on parameters
	if fromDate != nil {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(" AND p.updated_at >= '%s'", fromDate.Format(time.RFC3339Nano)))
		} else {
			whereCondition.WriteString(fmt.Sprintf(" p.updated_at >= '%s'", fromDate.Format(time.RFC3339Nano)))
		}
	}

	if paymentType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND LOWER(p."PaymentType")=LOWER(` + args.arg(paymentType) + `)`)
		} else {
			whereCondition.WriteString(` LOWER(p."PaymentType")=LOWER(` + args.arg(paymentType) + `)`)
		}
	}

	if paymentStatus != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND LOWER(p."PaymentStatus")=LOWER(` + args.arg(paymentStatus) + `)`)
		} else {
			whereCondition.WriteString(` LOWER(p."PaymentStatus")=LOWER(` + args.arg(paymentStatus) + `)`)
		}
	}

	if paymentsWithToken != nil {
		if *paymentsWithToken {
			if whereCondition.String() != "" {
				whereCondition.WriteString(" AND p.pelecard_token IS NOT NULL")
			} else {
				whereCondition.WriteString(" p.pelecard_token IS NOT NULL")
			}
		} else {
			if whereCondition.String() != "" {
				whereCondition.WriteString(" AND p.pelecard_token IS NULL")
			} else {
				whereCondition.WriteString(" p.pelecard_token IS NULL")
			}

		}

	}

	if orderType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND o.id = p."OrderID" AND LOWER(o."Type")=LOWER(` + args.arg(orderType) + `)`)
		} else {
			whereCondition.WriteString(` o.id = p."OrderID" AND LOWER(o."Type")=LOWER(` + args.arg(orderType) + `)`)
		}

	}

	// Email, account and keycloak id filter together: for a non-admin the
	// handler pins userKey to the caller, so neither email nor accontID may
	// replace it.
	if email != "" || accontID != 0 || userKey != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND p."OrderID" = o.id AND a.id = o."AccountID"`)
		} else {
			whereCondition.WriteString(` p."OrderID" = o.id AND a.id = o."AccountID"`)
		}
		if email != "" {
			whereCondition.WriteString(` AND LOWER(a."Email")=LOWER(` + args.arg(email) + `)`)
		}
		if accontID != 0 {
			whereCondition.WriteString(fmt.Sprintf(` AND a.id=%d`, accontID))
		}
		if userKey != "" {
			whereCondition.WriteString(` AND a."UserKey" = ` + args.arg(userKey))
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
			whereCondition.WriteString(` AND o.id = p."OrderID" AND LOWER(o."Type")=LOWER(` + args.arg(orderType) + `)`)
		} else {
			whereCondition.WriteString(` o.id = p."OrderID" AND LOWER(o."Type")=LOWER(` + args.arg(orderType) + `)`)
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
	return whereString.String(), orderBy.String(), args
}

func buildAndGetOfflinePaymentsWhereQuery(method string, orderByCreatedAt string) (string, string, []any, error) {
	var args queryArgs
	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	// WHERE query generation based on parameters
	if method != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(` AND p.payment_method=` + args.arg(method))
		} else {
			whereCondition.WriteString(` p.payment_method=` + args.arg(method))
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
	return whereString.String(), orderBy.String(), args, nil
}

func preparePaymentCreateQuery(req Payment) (string, string, queryArgs) {
	var c colVals

	if req.Amount.Valid {
		c.add(`"Amount"`, req.Amount.Float64)
	}
	if req.Currency.Valid {
		c.add(`"Currency"`, req.Currency.String)
	}
	if req.PaymentStatus.Valid {
		c.add(`"PaymentStatus"`, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		c.add(`"PaymentType"`, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		c.add(`"OrderID"`, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		c.add(`"ParamX"`, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		c.add(`"AuthNo"`, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		c.add("confirmation_key", req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		c.add("success", req.Success.String)
	}
	if req.PelecardToken.Valid && req.PelecardToken.String != "" {
		c.add("pelecard_token", req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		c.add(`"TransactionID"`, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		c.add(`"ErrorMsg"`, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		c.add(`"CardHebrewName"`, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		c.add(`"CCAbroadCard"`, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		c.add(`"CCBrand"`, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		c.add(`"CCCompanyClearer"`, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		c.add(`"CCCompanyIssuer"`, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		c.add("credit_type", req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		c.add(`"CCExpDate"`, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		c.add(`"CCNumber"`, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		c.add(`"DebitCode"`, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		c.add(`"DebitCurrency"`, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		c.add(`"DebitTotal"`, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		c.add(`"DebitType"`, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		c.add(`"FirstPaymentTotal"`, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		c.add(`"FixedPaymentTotal"`, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		c.add("j_param", req.JParam.String)
	}
	if req.TotalPayments.Valid {
		c.add(`"TotalPayments"`, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		c.add(`"TransactionInitTime"`, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		c.add(`"TransactionUpdateTime"`, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		c.add(`"VoucherID"`, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		c.add(`"Ordkey"`, req.Ordkey.String)
	}
	if req.PricingVersion.Valid {
		c.add("pricing_version", req.PricingVersion.String)
	}
	if req.PricingEvaluation.Valid {
		c.add("pricing_evaluation", req.PricingEvaluation.JSON)
	}
	if !c.empty() {
		c.add("created_at", time.Now())
		c.add("updated_at", time.Now())
	}

	return c.insertLists()
}

func preparePaymentUpdateQuery(req Payment) (string, queryArgs) {
	var c colVals

	if req.Amount.Valid {
		c.add(`"Amount"`, req.Amount.Float64)
	}
	if req.Currency.Valid {
		c.add(`"Currency"`, req.Currency.String)
	}
	if req.PaymentStatus.Valid {
		c.add(`"PaymentStatus"`, req.PaymentStatus.String)
	}
	if req.PaymentType.Valid {
		c.add(`"PaymentType"`, req.PaymentType.String)
	}
	if req.OrderID.Valid {
		c.add(`"OrderID"`, req.OrderID.Int)
	}
	if req.ParamX.Valid {
		c.add(`"ParamX"`, req.ParamX.String)
	}
	if req.AuthNo.Valid {
		c.add(`"AuthNo"`, req.AuthNo.String)
	}
	if req.ConfirmationKey.Valid {
		c.add("confirmation_key", req.ConfirmationKey.String)
	}
	if req.Success.Valid {
		c.add("success", req.Success.String)
	}
	if req.PelecardToken.Valid && req.PelecardToken.String != "" {
		c.add("pelecard_token", req.PelecardToken.String)
	}
	if req.TransactionID.Valid {
		c.add(`"TransactionID"`, req.TransactionID.String)
	}
	if req.ErrorMsg.Valid {
		c.add(`"ErrorMsg"`, req.ErrorMsg.String)
	}
	if req.CardHebrewName.Valid {
		c.add(`"CardHebrewName"`, req.CardHebrewName.String)
	}
	if req.CCAbroadCard.Valid {
		c.add(`"CCAbroadCard"`, req.CCAbroadCard.String)
	}
	if req.CCBrand.Valid {
		c.add(`"CCBrand"`, req.CCBrand.String)
	}
	if req.CCCompanyClearer.Valid {
		c.add(`"CCCompanyClearer"`, req.CCCompanyClearer.String)
	}
	if req.CCCompanyIssuer.Valid {
		c.add(`"CCCompanyIssuer"`, req.CCCompanyIssuer.String)
	}
	if req.CreditType.Valid {
		c.add("credit_type", req.CreditType.String)
	}
	if req.CCExpDate.Valid {
		c.add(`"CCExpDate"`, req.CCExpDate.String)
	}
	if req.CCNumber.Valid {
		c.add(`"CCNumber"`, req.CCNumber.String)
	}
	if req.DebitCode.Valid {
		c.add(`"DebitCode"`, req.DebitCode.String)
	}
	if req.DebitCurrency.Valid {
		c.add(`"DebitCurrency"`, req.DebitCurrency.String)
	}
	if req.DebitTotal.Valid {
		c.add(`"DebitTotal"`, req.DebitTotal.String)
	}
	if req.DebitType.Valid {
		c.add(`"DebitType"`, req.DebitType.String)
	}
	if req.FirstPaymentTotal.Valid {
		c.add(`"FirstPaymentTotal"`, req.FirstPaymentTotal.String)
	}
	if req.FixedPaymentTotal.Valid {
		c.add(`"FixedPaymentTotal"`, req.FixedPaymentTotal.String)
	}
	if req.JParam.Valid {
		c.add("j_param", req.JParam.String)
	}
	if req.TotalPayments.Valid {
		c.add(`"TotalPayments"`, req.TotalPayments.String)
	}
	if req.TransactionInitTime.Valid {
		c.add(`"TransactionInitTime"`, req.TransactionInitTime.String)
	}
	if req.TransactionUpdateTime.Valid {
		c.add(`"TransactionUpdateTime"`, req.TransactionUpdateTime.String)
	}
	if req.VoucherID.Valid {
		c.add(`"VoucherID"`, req.VoucherID.String)
	}
	if req.Ordkey.Valid {
		c.add(`"Ordkey"`, req.Ordkey.String)
	}
	if req.PricingVersion.Valid {
		c.add("pricing_version", req.PricingVersion.String)
	}
	if req.PricingEvaluation.Valid {
		c.add("pricing_evaluation", req.PricingEvaluation.JSON)
	}

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func (o *OrdersDB) FetchPaymentByParamX(ctx context.Context, paramX string) (*PaymentWithFullName, error) {
	var p PaymentWithFullName

	if err := o.pool.QueryRow(ctx, `select COALESCE(a."UserKey", ''), a.id, a."FirstName", a."LastName", COALESCE(a."Email", ''), a."Street", a."City", o."OrderLanguage", o."Amount", o."Currency", 
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

// blankAsNull treats an empty string as absent, so it is left unwritten
// rather than stored as ''.
func blankAsNull(s null.String) null.String {
	if s.String == "" {
		return null.String{}
	}
	return s
}
