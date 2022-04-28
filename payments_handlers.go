package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/jackc/pgx/v4"
)

func handleCreatePayment(c *gin.Context) {
	var p Payment
	bindErr := c.BindJSON(&p)
	if bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bindErr.Error()})
		return
	}
	fmt.Println(p)
	if p.OrderID.Int64 == 0 {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing OrderID"})
		return
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)

	if len(createQueryArgs) != 0 {
		_, err := DB.Exec(c, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s)`, createString, numString),
			createQueryArgs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			return
		}

		c.JSON(http.StatusOK, p)
		return
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Errorf("no values to insert")})
		return
	}
}

func handlePaymentFetchViaParamX(c *gin.Context) {

	var paramx = c.Param("paramx")

	payment, err := fetchPaymentByParamX(c, paramx)

	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, payment)
	}
}

func handlePaymentUpdate(c *gin.Context) {

	var req OfflineAndPelecardPayment
	errRequest := c.ShouldBindBodyWith(&req, binding.JSON)

	if errRequest != nil {
		log.Println("Err:", errRequest)
		c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest})
		return
	}

	if req.PaymentType.String == "manual" || req.PaymentType.String == "pelecard" {
		var offAndPeleReq OfflineAndPelecardPayment
		offAndPeleErrRequest := c.ShouldBindBodyWith(&offAndPeleReq, binding.JSON)

		if offAndPeleErrRequest != nil {
			log.Println("Err:", offAndPeleErrRequest)
			c.JSON(http.StatusBadRequest, gin.H{"Error": offAndPeleErrRequest.Error()})
			return
		}

		var err error

		if offAndPeleReq.PaymentType.String == "manual" {
			err = updateOfflinePayment(c, offAndPeleReq)
		} else {
			err = updatePelecardPayment(c, offAndPeleReq)
		}

		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
			return
		} else {
			c.Status(http.StatusOK)
			return
		}
	} else if req.PaymentType.String == "helphaver" {
		var helpReq HelpHavedPayment
		errRequest := c.ShouldBindBodyWith(&helpReq, binding.JSON)

		if errRequest != nil {
			log.Println("Err:", errRequest)
			c.JSON(http.StatusBadRequest, gin.H{"Error": errRequest})
			return
		}
		err := updateHelpHavePayment(c, helpReq)

		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Helphaver payment not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
			return
		} else {
			c.Status(http.StatusOK)
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"Error": "Invalid payment type"})
		return
	}
}

func handlePaymentFetchByEmail(c *gin.Context) {
	var email = c.Param("email")

	ord, err := getPaymentByEmail(c, email)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": ord, "success": true})
	}
}

func handleGetActivities(c *gin.Context) {
	skip := c.Query("skip")
	limit := c.Query("limit")
	email := c.Query("email")
	productType := c.Query("product-type")
	paymentType := c.Query("payment-type")

	if skip == "" {
		skip = "0"
	}

	if limit == "" {
		limit = "10"
	}

	// String conversion to int
	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	// String conversion to int
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	payAct, err := getPaymentActivities(c, email, productType, paymentType, intSkip, intLimit)
	count, _ := getTotalParticipationStatusCount(c, email, productType, paymentType)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payAct, "totalCount": count, "success": true})
	}
}

func handleUpdatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)

	if p.OrderID.Int64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing OrderID"})
		return
	}

	var pi Payment

	if err := DB.QueryRow(c, `select "OrderID" from payments where id = $1`, p.ID).Scan(
		&pi.OrderID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
	}

	if pi.OrderID.Int64 == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment ID not found"})
	}

	if p.OrderID != pi.OrderID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order ID mismatch"})
		return
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, p.ID),
			toUpdateArgs...)
		if err != nil {
			fmt.Errorf("problem updating payments: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			fmt.Println(updateRes.RowsAffected())
			c.JSON(http.StatusNotFound, gin.H{"error": "Payment not Saved"})
			return
		}
	} else {
		fmt.Println("invalid values")
	}

	c.JSON(http.StatusOK, p)

}

func fetchPaymentByParamX(ctx *gin.Context, paramX string) (paymentWithFullName, error) {
	var p paymentWithFullName

	if err := DB.QueryRow(ctx, `select a."UserKey", a.id, a."FirstName", a."LastName", a."Email", a."Street", a."City", o."OrderLanguage", o."Amount", o."Currency", 
	p.id, p."Amount", p."PaymentStatus", p."PaymentType", p."OrderID", p."ParamX", p."AuthNo", p.confirmation_key,
	p.success, p.pelecard_token, p."TransactionID", p."ErrorMsg", p."CardHebrewName", p."CCAbroadCard", p."CCBrand",
	p."CCCompanyClearer", p."CCCompanyIssuer", p.credit_type, p."CCExpDate", p."CCNumber", p."DebitCode", p."DebitCurrency",
	p."DebitTotal", p."DebitType", p."FirstPaymentTotal", p."FixedPaymentTotal", p.j_param, p."TotalPayments",
	p."TransactionInitTime", p."TransactionUpdateTime", p."VoucherID", p."Ordkey", p.created_at, p.updated_at, p.deleted_at 
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
		&p.Ordkey, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	); err != nil {
		fmt.Println("--err--", err)
		log.Printf("\n## ERROR - NO PAYMENT for ParamX %v\n", paramX)
		return p, err
	}

	return p, nil
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
		args = append(args, req.OrderID.Int64)
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
		args = append(args, req.OrderID.Int64)
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

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}
