package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goji/param"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handlePaymentFetchByID(c *gin.Context) {
	id := c.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	account, err := o.repo.GetPaymentByID(c, intID)

	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
			return
		}
		fmt.Println("Error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": account, "success": true})
		return
	}
}

func (o *OrdersAPI) handlePaymentDelete(c *gin.Context) {
	id := c.Param("id")

	var (
		intID int
		err   error
	)

	intID, err = strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = o.repo.SoftDeletePayment(c, intID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handlePaymentFetchViaParamX(c *gin.Context) {

	var paramx = c.Param("paramx")

	payment, err := o.repo.FetchPaymentByParamX(c, paramx)

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

func (o *OrdersAPI) handlePaymentUpdate(c *gin.Context) {

	var req repo.PaymentUpdate
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

	if req.PaymentID.IsZero() {
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

		err := o.repo.UpdateOfflinePayment(c, req)

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

		err := o.repo.UpdateHelpHavePayment(c, req)

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

		err := o.repo.UpdatePelecardPayment(c, req)

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
		orderId, parentPaymentUpdateErr := o.repo.UpdateParentPaymentTableStatusAndReturnOrderId(c, paymentStatus, req.PaymentID.Int)

		if parentPaymentUpdateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"Error": parentPaymentUpdateErr.Error()})
			return
		}

		if orderId != 0 && !req.RestrictOrderUpdate.Bool {
			o.repo.UpdateOrderStatusByOrderID(c, orderId, paymentStatus)
		}

		c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
		return
	}
}

func (o *OrdersAPI) handlePaymentFetch(c *gin.Context) {
	skip := c.Query("skip")
	limit := c.Query("limit")
	fromDate := c.Query("from-date")
	toDate := c.Query("to-date")
	paymentType := c.Query("p-type")
	paymentStatus := c.Query("status")
	orderType := c.Query("o-type")
	email := c.Query("email")
	accountID := c.Query("account-id")
	tokenExist := c.Query("token-exist")
	orderID := c.Query("order-id")
	orderByCreatedAt := c.Query("o-created-at")

	if orderByCreatedAt != "" {
		if orderByCreatedAt != "asc" && orderByCreatedAt != "desc" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order by created at"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date-from"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	// String conversion to int
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	if accountID != "" {
		intAccountID, err = strconv.Atoi(accountID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intAccountID = 0
	}

	if orderID != "" {
		intOrderID, err = strconv.Atoi(orderID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
			return
		}
	} else {
		intOrderID = 0
	}

	payments, err := o.repo.GetAllPayments(c, intSkip, intLimit, fromDate, &toDateParsed, paymentType, paymentStatus, orderType, email, intAccountID, tokenExist, intOrderID, orderByCreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payments, "success": true})
}

func (o *OrdersAPI) handlePaymentFetchByEmail(c *gin.Context) {
	var email = c.Param("email")

	ord, err := o.repo.GetPaymentByEmail(c, email)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": ord, "success": true})
	}
}

func (o *OrdersAPI) handleGetActivities(c *gin.Context) {
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

	payAct, err := o.repo.GetPaymentActivities(c, email, productType, paymentType, intSkip, intLimit)
	count, _ := o.repo.GetTotalParticipationStatusCount(c, email, productType, paymentType)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err})
	} else {
		c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payAct, "totalCount": count, "success": true})
	}
}
