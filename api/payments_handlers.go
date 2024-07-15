package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goji/param"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handlePaymentFetchByID(c *gin.Context) {
	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	payment, err := o.repo.GetPaymentByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetPaymentByID: %w", err))
		}
		return
	} else {
		if !isAdmin {
			account, err := o.repo.GetAccountForOrderID(c, uint(payment.OrderID.Int))
			if err != nil {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.GetAccountForOrderID: %w", err))
				return
			}
			if keycloakId != account.UserKey.String {
				c.Status(http.StatusForbidden)
				return
			}
		}

	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payment, "success": true})
}

func (o *OrdersAPI) handlePaymentDelete(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	err = o.repo.SoftDeletePayment(c.Request.Context(), id)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.SoftDeletePayment: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})
}

func (o *OrdersAPI) handlePaymentFetchViaParamX(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleAdmin, common.RoleRoot) {
		c.Status(http.StatusForbidden)
		return
	}
	paramx := c.Param("paramx")
	payment, err := o.repo.FetchPaymentByParamX(c.Request.Context(), paramx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.FetchPaymentByParamX: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, payment)
}

func (o *OrdersAPI) handlePaymentUpdate(c *gin.Context) {
	if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
		return
	}
	var req repo.PaymentUpdate
	var paymentStatus string

	err := c.Request.ParseMultipartForm(32 << 20) // 32 MB
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parseErr := param.Parse(c.Request.MultipartForm.Value, &req)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": parseErr.Error()})
		return
	}

	if req.PaymentID.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing PaymentID"})
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
			utils.LogFor(c.Request.Context()).Info("upload file to storage",
				slog.Group("file", slog.String("name", fileName), slog.Int64("size", size)))

			fileUrl, uploadErr := utils.UploadFileToS3(buffer, fileName)
			if uploadErr != nil {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("utils.UploadFileToS3: %w", uploadErr))
				return
			}

			req.Receipt = null.NewString(fileUrl, true)
		}

		err := o.repo.UpdateOfflinePayment(c.Request.Context(), req)
		if err != nil {
			if errors.Is(err, common.ErrNoRowsAffected) {
				c.Status(http.StatusNotFound)
			} else if errors.Is(err, common.ErrInvalidValues) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.UpdateOfflinePayment: %w", err))
			}
			return
		}
	} else if req.PaymentType.String == "helphaver" {
		if req.Status.String != "" {
			paymentStatus = req.Status.String
		}

		err := o.repo.UpdateHelpHavePayment(c.Request.Context(), req)
		if err != nil {
			if errors.Is(err, common.ErrNoRowsAffected) {
				c.Status(http.StatusNotFound)
			} else if errors.Is(err, common.ErrInvalidValues) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.UpdateHelpHavePayment: %w", err))
			}
			return
		}
	} else if req.PaymentType.String == "pelecard" {
		if req.PaymentStatus.String != "" {
			paymentStatus = req.PaymentStatus.String
		}

		err := o.repo.UpdatePelecardPayment(c.Request.Context(), req)
		if err != nil {
			if errors.Is(err, common.ErrNoRowsAffected) {
				c.Status(http.StatusNotFound)
			} else if errors.Is(err, common.ErrInvalidValues) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			} else {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.UpdatePelecardPayment: %w", err))
			}
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"קrror": "Invalid payment type"})
		return
	}

	// Updating the status of the parent payment table.
	if paymentStatus != "" {
		orderId, err := o.repo.UpdateParentPaymentTableStatusAndReturnOrderId(c, paymentStatus, req.PaymentID.Int)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.UpdateParentPaymentTableStatusAndReturnOrderId: %w", err))
			return
		}

		if orderId != 0 && !req.RestrictOrderUpdate.Bool {
			err = o.repo.UpdateOrderStatusByOrderID(c.Request.Context(), orderId, paymentStatus)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				_ = c.Error(fmt.Errorf("repo.UpdateOrderStatusByOrderID: %w", err))
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})
		return
	}
}

func (o *OrdersAPI) handlePaymentFetch(c *gin.Context) {
	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}

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

	var (
		currentUserAccountId int
		err                  error
	)
	if !isAdmin {
		currentUserAccountId, err = o.repo.GetAccountIDByKeycloakID(c, keycloakId)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("o.repo.GetAccountIDByKeycloakID: %w", err))
			return
		}

	}

	if orderByCreatedAt != "" {
		if orderByCreatedAt != "asc" && orderByCreatedAt != "desc" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order by created at"})
			return
		}
	}

	var (
		toDateParsed time.Time
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

	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

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

		if !isAdmin && currentUserAccountId != intAccountID {
			c.Status(http.StatusForbidden)
			return
		}
	} else {
		if !isAdmin {
			intAccountID = currentUserAccountId
		}

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

	payments, err := o.repo.GetAllPayments(c.Request.Context(), intSkip, intLimit, fromDate, &toDateParsed, paymentType,
		paymentStatus, orderType, email, intAccountID, tokenExist, intOrderID, orderByCreatedAt)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetAllPayments: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payments, "success": true})
}

func (o *OrdersAPI) handlePaymentFetchByEmail(c *gin.Context) {

	var email = c.Param("email")

	if !o.isEmailOwnerOrHasAnyRole(c, email, common.RoleRoot, common.RoleAdmin) {
		return
	}
	ord, err := o.repo.GetPaymentByEmail(c.Request.Context(), email)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetPaymentByEmail: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": ord, "success": true})
}

func (o *OrdersAPI) handleGetActivities(c *gin.Context) {
	isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
	if !isAuthUser {
		return
	}
	var (
		currentUserEmail string
		err              error
	)
	if !isAdmin {
		currentUserEmail, err = o.repo.GetEmailByKeycloakID(c, keycloakId) // getting user email by keycloakId
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetEmailByKeycloakID: %w", err))
			return
		}
	}

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
	if !isAdmin && email != "" && currentUserEmail != email {
		c.Status(http.StatusForbidden)
		return
	}
	if !isAdmin {
		email = currentUserEmail
	}
	intSkip, err := strconv.Atoi(skip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid skip value! Accepted value is INTEGER", "success": false})
		return
	}

	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit value! Accepted value is INTEGER", "success": false})
		return
	}

	payAct, err := o.repo.GetPaymentActivities(c.Request.Context(), email, productType, paymentType, intSkip, intLimit)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetPaymentActivities: %w", err))
		return
	}

	count, err := o.repo.GetTotalParticipationStatusCount(c.Request.Context(), email, productType, paymentType)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.GetTotalParticipationStatusCount: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": payAct, "totalCount": count, "success": true})
}
