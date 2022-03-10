package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func handleCreatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)
	fmt.Println(p)
	if *p.OrderID == 0 {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing OrderID"})
		return
	}

	fmt.Println(p)

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

func handleUpdatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)

	if *p.OrderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing OrderID"})
		return
	}

	var pi Payment

	if err := DB.QueryRow(c, `select "OrderID" from payments where id = $1`, *p.ID).Scan(
		&pi.OrderID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
	}

	if pi.OrderID == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment ID not found"})
	}

	if p.OrderID != pi.OrderID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order ID mismatch"})
		return
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, *p.ID),
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

func preparePaymentCreateQuery(req Payment) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.Amount != nil {
		createStrings = append(createStrings, `"Amount"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Amount)
	}
	if req.PaymentStatus != nil {
		createStrings = append(createStrings, `"PaymentStatus"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentStatus)
	}
	if req.PaymentType != nil {
		createStrings = append(createStrings, `"PaymentType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentType)
	}
	if req.OrderID != nil {
		createStrings = append(createStrings, `"OrderID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.OrderID)
	}
	if req.ParamX != nil {
		createStrings = append(createStrings, `"ParamX"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.ParamX)
	}
	if req.AuthNo != nil {
		createStrings = append(createStrings, `"AuthNo"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.AuthNo)
	}
	if req.ConfirmationKey != nil {
		createStrings = append(createStrings, "confirmation_key")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.ConfirmationKey)
	}
	if req.Success != nil {
		createStrings = append(createStrings, "success")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Success)
	}
	if req.PelecardToken != nil {
		createStrings = append(createStrings, "pelecard_token")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PelecardToken)
	}
	if req.TransactionID != nil {
		createStrings = append(createStrings, `"TransactionID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.TransactionID)
	}
	if req.ErrorMsg != nil {
		createStrings = append(createStrings, `"ErrorMsg"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.ErrorMsg)
	}
	if req.CardHebrewName != nil {
		createStrings = append(createStrings, `"CardHebrewName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CardHebrewName)
	}
	if req.CCAbroadCard != nil {
		createStrings = append(createStrings, `"CCAbroadCard"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCAbroadCard)
	}
	if req.CCBrand != nil {
		createStrings = append(createStrings, `"CCBrand"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCBrand)
	}
	if req.CCCompanyClearer != nil {
		createStrings = append(createStrings, `"CCCompanyClearer"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCCompanyClearer)
	}
	if req.CCCompanyIssuer != nil {
		createStrings = append(createStrings, `"CCCompanyIssuer"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCCompanyIssuer)
	}
	if req.CreditType != nil {
		createStrings = append(createStrings, "credit_type")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CreditType)
	}
	if req.CCExpDate != nil {
		createStrings = append(createStrings, `"CCExpDate"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCExpDate)
	}
	if req.CCNumber != nil {
		createStrings = append(createStrings, `"CCNumber"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.CCNumber)
	}
	if req.DebitCode != nil {
		createStrings = append(createStrings, `"DebitCode"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.DebitCode)
	}
	if req.DebitCurrency != nil {
		createStrings = append(createStrings, `"DebitCurrency"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.DebitCurrency)
	}
	if req.DebitTotal != nil {
		createStrings = append(createStrings, `"DebitTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.DebitTotal)
	}
	if req.DebitType != nil {
		createStrings = append(createStrings, `"DebitType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.DebitType)
	}
	if req.FirstPaymentTotal != nil {
		createStrings = append(createStrings, `"FirstPaymentTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.FirstPaymentTotal)
	}
	if req.FixedPaymentTotal != nil {
		createStrings = append(createStrings, `"FixedPaymentTotal"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.FixedPaymentTotal)
	}
	if req.JParam != nil {
		createStrings = append(createStrings, "j_param")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.JParam)
	}
	if req.TotalPayments != nil {
		createStrings = append(createStrings, `"TotalPayments"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.TotalPayments)
	}
	if req.TransactionInitTime != nil {
		createStrings = append(createStrings, `"TransactionInitTime"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.TransactionInitTime)
	}
	if req.TransactionUpdateTime != nil {
		createStrings = append(createStrings, `"TransactionUpdateTime"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.TransactionUpdateTime)
	}
	if req.VoucherID != nil {
		createStrings = append(createStrings, `"VoucherID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.VoucherID)
	}
	if req.Ordkey != nil {
		createStrings = append(createStrings, `"Ordkey"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Ordkey)
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

	if req.Amount != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Amount"=$%d`, len(updateStrings)+1))
		args = append(args, *req.Amount)
	}
	if req.PaymentStatus != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"PaymentStatus"=$%d`, len(updateStrings)+1))
		args = append(args, *req.PaymentStatus)
	}
	if req.PaymentType != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"PaymentType"=$%d`, len(updateStrings)+1))
		args = append(args, *req.PaymentType)
	}
	if req.OrderID != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"OrderID"=$%d`, len(updateStrings)+1))
		args = append(args, *req.OrderID)
	}
	if req.ParamX != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"ParamX"=$%d`, len(updateStrings)+1))
		args = append(args, *req.ParamX)
	}
	if req.AuthNo != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"AuthNo"=$%d`, len(updateStrings)+1))
		args = append(args, *req.AuthNo)
	}
	if req.ConfirmationKey != nil {
		updateStrings = append(updateStrings, fmt.Sprintf("confirmation_key=$%d", len(updateStrings)+1))
		args = append(args, *req.ConfirmationKey)
	}
	if req.Success != nil {
		updateStrings = append(updateStrings, fmt.Sprintf("success=$%d", len(updateStrings)+1))
		args = append(args, *req.Success)
	}
	if req.PelecardToken != nil {
		updateStrings = append(updateStrings, fmt.Sprintf("pelecard_token=$%d", len(updateStrings)+1))
		args = append(args, *req.PelecardToken)
	}
	if req.TransactionID != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionID"=$%d`, len(updateStrings)+1))
		args = append(args, *req.TransactionID)
	}
	if req.ErrorMsg != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"ErrorMsg"=$%d`, len(updateStrings)+1))
		args = append(args, *req.ErrorMsg)
	}
	if req.CardHebrewName != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CardHebrewName"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CardHebrewName)
	}
	if req.CCAbroadCard != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCAbroadCard"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCAbroadCard)
	}
	if req.CCBrand != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCBrand"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCBrand)
	}
	if req.CCCompanyClearer != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCCompanyClearer"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCCompanyClearer)
	}
	if req.CCCompanyIssuer != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCCompanyIssuer"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCCompanyIssuer)
	}
	if req.CreditType != nil {
		updateStrings = append(updateStrings, fmt.Sprintf("credit_type=$%d", len(updateStrings)+1))
		args = append(args, *req.CreditType)
	}
	if req.CCExpDate != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCExpDate"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCExpDate)
	}
	if req.CCNumber != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"CCNumber"=$%d`, len(updateStrings)+1))
		args = append(args, *req.CCNumber)
	}
	if req.DebitCode != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitCode"=$%d`, len(updateStrings)+1))
		args = append(args, *req.DebitCode)
	}
	if req.DebitCurrency != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitCurrency"=$%d`, len(updateStrings)+1))
		args = append(args, *req.DebitCurrency)
	}
	if req.DebitTotal != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitTotal"=$%d`, len(updateStrings)+1))
		args = append(args, *req.DebitTotal)
	}
	if req.DebitType != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"DebitType"=$%d`, len(updateStrings)+1))
		args = append(args, *req.DebitType)
	}
	if req.FirstPaymentTotal != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"FirstPaymentTotal"=$%d`, len(updateStrings)+1))
		args = append(args, *req.FirstPaymentTotal)
	}
	if req.FixedPaymentTotal != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"FixedPaymentTotal"=$%d`, len(updateStrings)+1))
		args = append(args, *req.FixedPaymentTotal)
	}
	if req.JParam != nil {
		updateStrings = append(updateStrings, fmt.Sprintf("j_param=$%d", len(updateStrings)+1))
		args = append(args, *req.JParam)
	}
	if req.TotalPayments != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TotalPayments"=$%d`, len(updateStrings)+1))
		args = append(args, *req.TotalPayments)
	}
	if req.TransactionInitTime != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionInitTime"=$%d`, len(updateStrings)+1))
		args = append(args, *req.TransactionInitTime)
	}
	if req.TransactionUpdateTime != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"TransactionUpdateTime"=$%d`, len(updateStrings)+1))
		args = append(args, *req.TransactionUpdateTime)
	}
	if req.VoucherID != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"VoucherID"=$%d`, len(updateStrings)+1))
		args = append(args, *req.VoucherID)
	}
	if req.Ordkey != nil {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Ordkey"=$%d`, len(updateStrings)+1))
		args = append(args, *req.Ordkey)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}
