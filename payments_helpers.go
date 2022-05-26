package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/guregu/null.v4"
)

func getPaymentByID(ctx *gin.Context, id int) (Payment, error) {
	var pay Payment

	if err := DB.QueryRow(ctx, `SELECT 
	id, created_at, updated_at, deleted_at, "Amount", "PaymentStatus", "PaymentType", "OrderID", "ParamX", "Ordkey", "AuthNo", 
	confirmation_key, success, pelecard_token, "TransactionID", "ErrorMsg", "CardHebrewName", "CCAbroadCard", "CCBrand", 
	"CCCompanyClearer", "CCCompanyIssuer", credit_type, "CCExpDate", "CCNumber", "DebitCode", "DebitCurrency", "DebitTotal", "DebitType", 
	"FirstPaymentTotal", "FixedPaymentTotal", "TotalPayments", j_param, "TransactionInitTime", "TransactionUpdateTime", "VoucherID" from payments where id = $1`, id).Scan(
		&pay.ID, &pay.CreatedAt, &pay.UpdatedAt, &pay.DeletedAt, &pay.Amount, &pay.PaymentStatus, &pay.PaymentType, &pay.OrderID, &pay.ParamX, &pay.Ordkey, &pay.AuthNo,
		&pay.ConfirmationKey, &pay.Success, &pay.PelecardToken, &pay.TransactionID, &pay.ErrorMsg, &pay.CardHebrewName, &pay.CCAbroadCard, &pay.CCBrand,
		&pay.CCCompanyClearer, &pay.CCCompanyIssuer, &pay.CreditType, &pay.CCExpDate, &pay.CCNumber, &pay.DebitCode, &pay.DebitCurrency, &pay.DebitTotal, &pay.DebitType,
		&pay.FirstPaymentTotal, &pay.FixedPaymentTotal, &pay.TotalPayments, &pay.JParam, &pay.TransactionInitTime, &pay.TransactionUpdateTime, &pay.VoucherID); err != nil {
		return pay, err
	}
	return pay, nil

}

func softDeletePayment(c *gin.Context, paymentID int) error {
	_, err := DB.Exec(c, "UPDATE payments SET deleted_at = $1 WHERE id = $2", time.Now(), paymentID)
	return err
}

func getPaymentActivities(ctx *gin.Context, email string, productType string, paymentType string, skip int, limit int) ([]PaymentActivitiesRes, error) {

	PaymentActivities := []PaymentActivitiesRes{}

	userDbWhereQuery, orderByQuery := buildAndGetWherePaymentActQuery(email, productType, paymentType)

	rows, err := DB.Query(ctx, `SELECT p.created_at,  p."Amount", p."PaymentType",  p."OrderID", 
	p."ParamX", p."PaymentStatus", p."CCNumber", p."CCExpDate", 
	o."ProductType", o."Type", o."Currency",
	a."FirstName", a."LastName", a."Email", a."Country" 
	from payments as p, orders as o, accounts as a`+userDbWhereQuery+
		orderByQuery+
		" LIMIT $1 OFFSET $2", limit, skip)
	if err != nil {
		return PaymentActivities, err
	}

	defer rows.Close()

	for rows.Next() {

		var p PaymentActivitiesRes

		err := rows.Scan(&p.CreatedAt, &p.Amount, &p.PaymentType, &p.OrderID, &p.ParamX, &p.PaymentStatus, &p.CCNumber, &p.CCExpDate, &p.ProductType, &p.Type, &p.Currency, &p.FirstName, &p.LastName, &p.Email, &p.Country)

		if err != nil {
			fmt.Println("--error while scanning payment activities res--", err)
			return PaymentActivities, err
		}

		PaymentActivities = append(PaymentActivities, p)
	}

	return PaymentActivities, nil
}

func GetAllPayments(ctx *gin.Context, skip int, limit int, fromDate string, toDate *time.Time, paymentType string, paymentStatus string, orderType string, email string, accountID int) (*[]Payment, error) {

	payments := []Payment{}

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)

	whereQuery, orderByQuery, queryBuildErr := buildAndGetPaymentsWhereQuery(fromDate, toDate, paymentType, paymentStatus, orderType, email, accountID)

	if queryBuildErr != nil {
		return &payments, queryBuildErr
	}

	fromQuery := " FROM payments as p"

	if email != "" || accountID != 0 || orderType != "" {
		fromQuery = fromQuery + ", orders as o"
		if email != "" || accountID != 0 {
			fromQuery = fromQuery + ", accounts as a"
		}
	}

	rows, err := DB.Query(ctx, `SELECT 
	p.id, p.created_at, p.updated_at, p.deleted_at, p."Amount", p."PaymentStatus", p."PaymentType", p."OrderID", p."ParamX", p."Ordkey", p."AuthNo", p.
	confirmation_key, p.success, p.pelecard_token, p."TransactionID", p."ErrorMsg", p."CardHebrewName", p."CCAbroadCard", p."CCBrand", p.
	"CCCompanyClearer", p."CCCompanyIssuer", p.credit_type, p."CCExpDate", p."CCNumber", p."DebitCode", p."DebitCurrency", p."DebitTotal", p."DebitType", p.
	"FirstPaymentTotal", p."FixedPaymentTotal", p."TotalPayments", p.j_param, p."TransactionInitTime", p."TransactionUpdateTime", p."VoucherID"
	`+fromQuery+whereQuery+orderByQuery+limitOffsetString)
	if err != nil {
		fmt.Println("--error-while-executing-query", err)
		return &payments, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Payment
		err := rows.Scan(
			&d.ID, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &d.Amount, &d.PaymentStatus, &d.PaymentType, &d.OrderID, &d.ParamX, &d.Ordkey, &d.AuthNo,
			&d.ConfirmationKey, &d.Success, &d.PelecardToken, &d.TransactionID, &d.ErrorMsg, &d.CardHebrewName, &d.CCAbroadCard, &d.CCBrand,
			&d.CCCompanyClearer, &d.CCCompanyIssuer, &d.CreditType, &d.CCExpDate, &d.CCNumber, &d.DebitCode, &d.DebitCurrency, &d.DebitTotal, &d.DebitType,
			&d.FirstPaymentTotal, &d.FixedPaymentTotal, &d.TotalPayments, &d.JParam, &d.TransactionInitTime, &d.TransactionUpdateTime, &d.VoucherID,
		)
		if err != nil {
			return &payments, err
		}
		payments = append(payments, d)
	}
	return &payments, rows.Err()

}

func getTotalParticipationStatusCount(ctx *gin.Context, email string, productType string, paymentType string) (int, error) {
	var count int

	userDbWhereQuery, _ := buildAndGetWherePaymentActQuery(email, productType, paymentType)

	err := DB.QueryRow(ctx, `SELECT COUNT(*) FROM payments as p, orders as o, accounts as a 
	`+userDbWhereQuery).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func getPaymentByEmail(ctx *gin.Context, email string) ([]PaymentByEmail, error) {

	paymentData := []PaymentByEmail{}

	rows, err := DB.Query(ctx, `select p.created_at, o."PaymentDate", o."Type", o."ProductType", o."Amount", o."Currency", p."CCNumber", p."ParamX", p."PaymentStatus"
	from payments as p, orders as o, accounts as a
	where a."Email" = $1
	and a.id = o."AccountID"
	and o.id = p."OrderID"
	order by p.created_at desc`, email)
	if err != nil {
		return paymentData, err
	}

	defer rows.Close()

	for rows.Next() {

		var p PaymentByEmail
		var amount string

		err := rows.Scan(&p.CreatedAt, &p.PaymentDate, &p.Type, &p.ProductType, &amount, &p.Currency, &p.CCNumber, &p.PaymentID, &p.PaymentStatus)

		if err != nil {
			return paymentData, err
		}

		value, err := strconv.ParseFloat(amount, 32)

		if err != nil {
			fmt.Println("error converting amount string to float")
			return paymentData, err
		}

		floatAmount := float64(value)

		p.Amount = null.NewFloat(floatAmount, true)

		paymentData = append(paymentData, p)
	}

	return paymentData, nil
}

func createOfflinePayment(c *gin.Context, req RequestOrder, paymentID uint, status string) error {

	createString, numString, createQueryArgs := prepareOfflinePaymentCreateQuery(req, paymentID, status)

	if len(createQueryArgs) != 0 {
		_, err := DB.Exec(c, fmt.Sprintf(`INSERT INTO payments_offline (%s) VALUES (%s)`, createString, numString),
			createQueryArgs...)
		if err != nil {
			return fmt.Errorf("problem creating offline payment: %w", err)
		}

		return nil
	} else {
		return fmt.Errorf("invalid values")
	}

}

func createPelecardPayment(c *gin.Context, req RequestOrder, paymentID uint, p Payment) error {

	createString, numString, createQueryArgs := preparePelecardPaymentCreateQuery(p, paymentID)

	if len(createQueryArgs) != 0 {
		_, err := DB.Exec(c, fmt.Sprintf(`INSERT INTO payments_pelecard (%s) VALUES (%s)`, createString, numString),
			createQueryArgs...)
		if err != nil {
			return fmt.Errorf("problem creating offline payment: %w", err)
		}

		return nil
	} else {
		return fmt.Errorf("invalid values")
	}

}

func preparePelecardPaymentCreateQuery(req Payment, paymentID uint) (string, string, []interface{}) {
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
		args = append(args, req.OrderID.Int64)
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

func preparePelecardPaymentUpdateQuery(req Payment) (string, []interface{}) {
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
		args = append(args, req.OrderID.Int64)
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

func createHelpHaverPayment(c *gin.Context, req RequestOrder, paymentID uint, status string) error {

	createString, numString, createQueryArgs := prepareHelpHaverPaymentCreateQuery(req, paymentID, status)

	if len(createQueryArgs) != 0 {
		_, err := DB.Exec(c, fmt.Sprintf(`INSERT INTO payments_helphaver (%s) VALUES (%s)`, createString, numString),
			createQueryArgs...)
		if err != nil {
			return fmt.Errorf("problem creating helphaver payment: %w", err)
		}

		return nil
	} else {
		return fmt.Errorf("invalid values")
	}

}

func updatePelecardPayment(c *gin.Context, req Payment, paymentID int) error {

	toUpdate, toUpdateArgs := preparePelecardPaymentUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdate, paymentID),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating pelecard payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("pelecard payment not updated as no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func updateOfflinePayment(c *gin.Context, req OfflinePayment) error {

	toUpdate, toUpdateArgs := prepareOfflinePaymentUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments_offline SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int64),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("Payment not Updated")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func updateHelpHavePayment(c *gin.Context, req HelpHavedPayment) error {

	toUpdate, toUpdateArgs := prepareHelpHaverPaymentUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments_helphaver SET %s WHERE payment_id=%d`, toUpdate, req.PaymentID.Int64),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("Payment not Updated")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func prepareOfflinePaymentCreateQuery(req RequestOrder, paymentID uint, status string) (string, string, []interface{}) {
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
	if status != "" {
		createStrings = append(createStrings, `status`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, status)
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}

func prepareOfflinePaymentUpdateQuery(req OfflinePayment) (string, []interface{}) {
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
func prepareHelpHaverPaymentCreateQuery(req RequestOrder, paymentID uint, status string) (string, string, []interface{}) {
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

func prepareHelpHaverPaymentUpdateQuery(req HelpHavedPayment) (string, []interface{}) {
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

	whereCondition.WriteString(" p.\"OrderID\" = o.id AND o.\"AccountID\" = a.id")

	// WHERE query generation based on parameters
	if email != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(a.\"Email\") LIKE LOWER('%%%s%%')", email))
	}

	if productType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(" AND LOWER(o.\"ProductType\")=LOWER('%s')", productType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(" LOWER(o.\"ProductType\")=LOWER('%s')", productType))
		}
	}

	if paymentType != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(fmt.Sprintf(" AND LOWER(p.\"PaymentType\")=LOWER('%s')", paymentType))
		} else {
			whereCondition.WriteString(fmt.Sprintf(" LOWER(p.\"PaymentType\")=LOWER('%s')", paymentType))
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

func buildAndGetPaymentsWhereQuery(fromDate string, dateTo *time.Time, paymentType string, paymentStatus string, orderType string, email string, accontID int) (string, string, error) {

	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	whereCondition.WriteString(fmt.Sprintf(" p.updated_at <= '%s'", dateTo.Format("2006-01-02 15:04:05")))

	// WHERE query generation based on parameters
	if fromDate != "" {
		rfcLayout := time.RFC3339
		fromDateParsed, err := time.Parse(rfcLayout, fromDate)

		if err != nil {
			return "", "", err
		}
		whereCondition.WriteString(fmt.Sprintf(" AND p.updated_at >= '%s'", fromDateParsed.Format("2006-01-02 15:04:05")))
	}

	if paymentType != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(p.\"PaymentType\")=LOWER('%s')", paymentType))
	}

	if paymentStatus != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND LOWER(p.\"PaymentStatus\")=LOWER('%s')", paymentStatus))
	}

	if orderType != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND o.id = p.\"OrderID\" AND LOWER(o.\"Type\")=LOWER('%s')", orderType))
	}

	if email != "" || accontID != 0 {
		if email != "" {
			whereCondition.WriteString(fmt.Sprintf(" AND p.\"OrderID\" = o.id AND a.id = o.\"AccountID\" AND LOWER(a.\"Email\")=LOWER('%s')", email))
		} else {
			whereCondition.WriteString(fmt.Sprintf(" AND p.\"OrderID\" = o.id AND a.id = o.\"AccountID\" AND a.id=%d", accontID))
		}
	}

	if orderType != "" {
		whereCondition.WriteString(fmt.Sprintf(" AND o.id = p.\"OrderID\" AND LOWER(o.\"Type\")=LOWER('%s')", orderType))
	}

	orderBy.WriteString(fmt.Sprintf(" ORDER BY p.updated_at %s", "desc"))

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}
