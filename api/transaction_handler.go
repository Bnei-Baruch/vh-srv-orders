package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func (o *OrdersAPI) handleTransactionGetByID(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
		return
	}

	transaction, err := o.repo.GetTransactionById(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.GetTransactionById: %w", err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": transaction, "success": true})
}

func (o *OrdersAPI) handleTransactionOrderAndPay(c *gin.Context) {
	var req repo.RequestOrder
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ord, err := o.repo.CreateOrderViaTransaction(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, common.ErrInvalidValues) {
			c.Status(http.StatusBadRequest)
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CreateOrderViaTransaction: %w", err))
		}
		return
	}

	req.PaymentStatus = null.StringFrom(common.PaymentStatusPending) // don't let anybody fool us
	p, err := o.repo.CreatePayment(c.Request.Context(), req, ord.ID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.CreatePayment: %w", err))
		return
	}

	if req.TerminalId.String == "" {
		if p.PaymentType.String == "pelecard" {
			if strings.ToLower(req.Type.String) == "recurring" {
				req.TerminalId = null.StringFrom("ben_recurring_pelecard")
			} else {
				req.TerminalId = null.StringFrom("ben_regular_pelecard")
			}
		}

		if p.PaymentType.String == "helphaver" {
			req.TerminalId = null.StringFrom("ben_helphaver")
		}
		if p.PaymentType.String == "offline" {
			req.TerminalId = null.StringFrom("ben_offline")
		}
	}

	tran := repo.Transaction{
		OrderID:    null.IntFrom(ord.ID),
		AccountID:  ord.AccountID,
		PaymentID:  null.IntFrom(p.ID),
		TerminalID: req.TerminalId,
	}

	_, err = o.repo.CreateTransactionAndGetId(c.Request.Context(), tran)
	if err != nil {
		if errors.Is(err, fmt.Errorf("invalid body")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.Status(http.StatusInternalServerError)
			_ = c.Error(fmt.Errorf("repo.CreateTransactionAndGetId: %w", err))
		}
		return
	}

	if (req.PaymentType.String == "offline" || req.PaymentType.String == "helphaver") && req.PaymentType.Valid {
		c.JSON(http.StatusCreated, gin.H{"Order": ord, "Payment": p})
		return
	}

	paramx := "m-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX")
	ordkey := "ord-" + strconv.FormatUint(uint64(ord.ID), 10) + os.Getenv("SUFX")

	errorurl := req.ErrorURL.String + "/" + ordkey + "/" + paramx
	cancelurl := req.CancelURL.String + "/" + ordkey + "/" + paramx

	extPay := repo.RequestPayment{
		UserKey: ordkey,

		GoodURL:   req.SuccessURL.String,
		ErrorURL:  errorurl,
		CancelURL: cancelurl,

		Name:         req.FirstName.String + " " + req.LastName.String,
		Price:        req.Amount.Float64,
		Currency:     req.Currency.String,
		Email:        req.Email.String,
		Phone:        "+NA",
		Street:       req.Street.String,
		City:         req.City.String,
		Country:      "Undef",
		Participans:  "1",
		Details:      req.Reference.String,
		SKU:          req.SKU.String,
		VAT:          "f",
		Installments: 1,
		Language:     req.OrderLanguage.String,
		Reference:    paramx,
		Organization: req.Organization.String,
	}

	if req.TerminalId.String == "ben_dummy_pelecard" {
		c.JSON(http.StatusOK, gin.H{"url": req.SuccessURL.String + "?success=1&token=1111111111&authNo=2222222&additional_details_param_x=" + paramx +
			"&card_hebrew_name=xxxxx&confirmation_key=xxxxxxxxx&credit_card_abroad_card=1&credit_card_brand=2&credit_card_company_clearer=1&credit_card_company_issuer=0&credit_card_exp_date=0925&credit_card_number=xxxxxxxxxxxxxxxx&credit_type=1&debit_code=50&debit_currency=2&debit_total=" + strconv.FormatFloat(req.Amount.Float64, 'f', 2, 64) +
			"&debit_type=1&first_payment_total=0&fixed_payment_total=0&j_param=4&station_number=1&total_payments=1&transaction_id=xxxxx-xxxxx-xxxx-xxxx-xxxxxxxx&transaction_init_time=" + time.Now().Format("2006-01-02 15:04:05") +
			"&transaction_pelecard_id=11111111&transaction_update_time=" + time.Now().Format("2006-01-02 15:04:05") +
			"&user_key=" + ordkey + "&voucher_id=00-000-000"})
		return
	}

	utils.LogFor(c.Request.Context()).Info("payment request", slog.Any("payload", extPay))

	payload, err := json.Marshal(extPay)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("json.Marshal repo.RequestPayment: %w", err))
		return
	}

	ENDPOINT := ""
	if req.Type.String == "recurring" {
		ENDPOINT = "https://checkout.kbb1.com/token/new"
	}
	if req.Type.String == "regular" {
		ENDPOINT = "https://checkout.kbb1.com/emv/new"
	}
	if req.Reference.String == "testemv" {
		ENDPOINT = "https://checkout.kbb1.com/emv/new"
	}
	utils.LogFor(c.Request.Context()).Debug("payment endpoint", slog.String("endpoint", ENDPOINT))

	resp, err := utils.PostJSON("POST", ENDPOINT, payload)
	if err != nil {
		utils.LogFor(c.Request.Context()).Info("POST external payment failed", slog.Any("err", err))
		c.JSON(http.StatusOK, gin.H{"url": errorurl})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	utils.LogFor(c.Request.Context()).Info("payment response", slog.Group("response",
		slog.String("status", resp.Status),
		slog.Any("headers", resp.Header),
		slog.String("body", string(body))))

	// Grisha you should fix that one... seriously
	if req.Type.String == "regular" {
		// if req.Type is regular - endpoint return some ass-shit string
		// gota parse the m*fkr

		var serRes repo.OrderServiceEmvRes
		if err := json.Unmarshal(body, &serRes); err != nil {
			utils.LogFor(c.Request.Context()).Info("payment response [regular] is not structured", slog.Any("err", err))
		}

		if serRes.Status == "success" {
			c.JSON(http.StatusOK, gin.H{"url": serRes.URL})
		} else {
			var i interface{}
			if err := json.Unmarshal(body, &i); err != nil {
				utils.LogFor(c.Request.Context()).Warn("payment response [regular] json.Unmarshal", slog.Any("err", err))
				utils.SentryFor(c.Request.Context()).CaptureException(err)
			}
			c.JSON(http.StatusOK, i)
		}

	} else {
		var i interface{}
		if err := json.Unmarshal(body, &i); err != nil {
			utils.LogFor(c.Request.Context()).Warn("payment response [generic] json.Unmarshal", slog.Any("err", err))
			utils.SentryFor(c.Request.Context()).CaptureException(err)
		}
		c.JSON(http.StatusOK, i)
	}
}

func (o *OrdersAPI) handleTransactionPaid(c *gin.Context) {
	var rp repo.RequestPaid
	if err := c.ShouldBindJSON(&rp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(rp.UserKey.String) == 0 {
		utils.LogFor(c.Request.Context()).Warn("user_key is empty", slog.String("additional_details_param_x", rp.ParamX.String))
		hub := utils.SentryFor(c.Request.Context())
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("additional_details_param_x", rp.ParamX.String)
			hub.CaptureMessage("OrdersAPI.handleTransactionPaid user_key is empty")
		})
		c.JSON(http.StatusBadRequest, gin.H{"error": "No order id provided in UserKey"})
		return
	}

	p, err := o.repo.UpdatePayment(c.Request.Context(), rp)
	if err != nil {
		// TODO : ask grisha to return more info on error
		utils.LogFor(c.Request.Context()).Warn("repo.UpdatePayment", slog.Any("err", err))
		utils.SentryFor(c.Request.Context()).CaptureException(err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	err = o.repo.UpdateOrderAfterPayment(c.Request.Context(), *p)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		_ = c.Error(fmt.Errorf("repo.UpdateOrderAfterPayment: %w", err))
		return
	}

	c.JSON(http.StatusOK, nil)
}
