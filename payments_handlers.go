package main

import (
	"fmt"
	"log"
	"net/http"

	"orderservices/orders/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goji/param"
	"github.com/jackc/pgx/v4"
	"gopkg.in/guregu/null.v4"
)

func handlePaymentFetchByID(ctx *gin.Context) {
	id := ctx.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	account, err := getPaymentByID(ctx, intID)

	if err != nil {
		if err == pgx.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
			return
		}
		fmt.Println("Error:", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": account, "success": true})
		return
	}
}

func handlePaymentDelete(ctx *gin.Context) {
	id := ctx.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = softDeletePayment(ctx, intID)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

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
		if err := DB.QueryRow(c, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
			createQueryArgs...).Scan(
			&p.ID,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": p, "success": true})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Errorf("no values to insert")})
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

	var req PaymentUpdate
	var paymentStatus string

	err := c.Request.ParseMultipartForm(32 << 20) // 32 MB
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parseErr := param.Parse(c.Request.MultipartForm.Value, &req)
	if parseErr != nil {
		fmt.Println(parseErr)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.PaymentID.Int64 == 0 {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing PaymentID"})
		return
	}

	if req.PaymentType.String == "offline" {

		if req.Status.String != "" {
			paymentStatus = req.Status.String
		}

		file, header, formErr := c.Request.FormFile("receipt")
		if formErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": formErr.Error()})
			return
		}

		if header.Size != 0 {
			fileName := "receipt/" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10) + "-" + header.Filename

			// get the file size and read
			// the file content into a buffer
			size := header.Size
			buffer := make([]byte, size)
			file.Read(buffer)

			// Uploading the file to AWS S3 bucket.
			fileUrl, uploadErr := utils.UploadFileToS3(buffer, fileName)

			if uploadErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": uploadErr.Error()})
				return
			}

			req.Receipt = null.NewString(fileUrl, true)
		}

		err := updateOfflinePayment(c, req)

		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
			return
		}
	} else if req.PaymentType.String == "helphaver" {

		if req.Status.String != "" {
			paymentStatus = req.Status.String
		}

		err := updateHelpHavePayment(c, req)

		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Helphaver payment not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
			return
		}
	} else if req.PaymentType.String == "pelecard" {

		if req.PaymentStatus.String != "" {
			paymentStatus = req.PaymentStatus.String
		}

		err := updatePelecardPayment(c, req)

		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Pelecard payment not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
			return
		}

	} else {
		c.JSON(http.StatusBadRequest, gin.H{"Error": "Invalid payment type"})
		return
	}

	// Updating the status of the parent payment table.
	if paymentStatus != "" {
		orderId, parentPaymentUpdateErr := updateParentPaymentTableStatusAndReturnOrderId(c, paymentStatus, req.PaymentID.Int64)

		if parentPaymentUpdateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"Error": parentPaymentUpdateErr.Error()})
			return
		}

		if orderId != 0 && !req.RestrictOrderUpdate.Bool {
			updateOrderStatusByOrderID(c, orderId, paymentStatus)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
		return
	}
}

func handlePaymentFetch(ctx *gin.Context) {
	skip := ctx.Query("skip")
	limit := ctx.Query("limit")
	fromDate := ctx.Query("from-date")
	toDate := ctx.Query("to-date")
	paymentType := ctx.Query("p-type")
	paymentStatus := ctx.Query("status")
	orderType := ctx.Query("o-type")
	email := ctx.Query("email")
	accountID := ctx.Query("account-id")
	tokenExist := ctx.Query("token-exist")
	orderID := ctx.Query("order-id")
	orderByCreatedAt := ctx.Query("o-created-at")

	if orderByCreatedAt != "" {
		if orderByCreatedAt != "asc" && orderByCreatedAt != "desc" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order by created at"})
			return
		}
	}

	var (
		toDateParsed time.Time
		err          error
		intAccountID int
		intOrderID   int
	)

	if toDate != "" {
		rfcLayout := time.RFC3339
		toDateParsed, err = time.Parse(rfcLayout, toDate)

		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date-from"})
		}
	} else {
		// set null value
		toDateParsed = time.Time{}
	}

	if skip == "" {
		skip = "0"
	}

	if limit == "" {
		limit = "10"
	}

	// String conversion to int
	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	// String conversion to int
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	if accountID != "" {
		intAccountID, err = strconv.Atoi(accountID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intAccountID = 0
	}

	if orderID != "" {
		intOrderID, err = strconv.Atoi(orderID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intOrderID = 0
	}

	payments, err := GetAllPayments(ctx, intSkip, intLimit, fromDate, &toDateParsed, paymentType, paymentStatus, orderType, email, intAccountID, tokenExist, intOrderID, orderByCreatedAt)

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payments, "success": true})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			return
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
