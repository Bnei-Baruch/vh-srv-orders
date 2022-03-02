package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func handleCreatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)
	fmt.Println(p)
	if p.OrderID == 0 {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Missing OrderID"})
		return
	}

	fmt.Println(p)

	execRes, err := DB.Exec(c,
		`INSERT INTO payments (
			Amount,
			PaymentStatus,
			PaymentType,
			OrderID,
			ParamX,
			AuthNo,
			confirmation_key,
			success,
			pelecard_token,
			TransactionID,
			ErrorMsg,
			CardHebrewName,
			CCAbroadCard,
			CCBrand,
			CCCompanyClearer,
			CCCompanyIssuer,
			credit_type,
			CCExpDate,
			CCNumber,
			DebitCode,
			DebitCurrency,
			DebitTotal,
			DebitType,
			FirstPaymentTotal,
			FixedPaymentTotal,
			j_param,
			TotalPayments,
			TransactionInitTime,
			TransactionUpdateTime,
			VoucherID,
			Ordkey,
			created_at,
			updated_at
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,
			$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34
			)`,
		p.Amount, p.PaymentStatus, p.PaymentType, p.OrderID, p.ParamX, p.AuthNo, p.ConfirmationKey, p.Success, p.PelecardToken,
		p.TransactionID, p.ErrorMsg, p.CardHebrewName, p.CCAbroadCard, p.CCBrand, p.CCCompanyClearer, p.CCCompanyIssuer, p.CreditType,
		p.CCExpDate, p.CCNumber, p.DebitCode, p.DebitCurrency, p.DebitTotal, p.DebitType, p.FirstPaymentTotal, p.FixedPaymentTotal,
		p.JParam, p.TotalPayments, p.TransactionInitTime, p.TransactionUpdateTime, p.VoucherID, p.Ordkey, time.Now(), time.Now())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	if execRes.RowsAffected() == 0 {
		fmt.Println(execRes.RowsAffected)
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not created"})
		return
	}

	c.JSON(http.StatusOK, p)
}

func handleUpdatePayment(c *gin.Context) {
	var p Payment
	c.BindJSON(&p)

	if p.OrderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing OrderID"})
		return
	}

	var pi Payment

	if err := DB.QueryRow(c, `select OrderID from accounts where id = $1`, p.ID).Scan(
		&pi.OrderID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
	}

	if &pi.OrderID == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment ID not found"})
	}

	if p.OrderID != pi.OrderID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order ID mismatch"})
		return
	}

	updateRes, err := DB.Exec(c, `UPDATE payments 
		SET
		Amount=$1,
		PaymentStatus=$2,
		PaymentType=$3,
		OrderID=$4,
		ParamX=$5,
		AuthNo=$6,
		confirmation_key=$7,
		success=$8,
		pelecard_token=$9,
		TransactionID=$10,
		ErrorMsg=$11,
		CardHebrewName=$12,
		CCAbroadCard=$13,
		CCBrand=$14,
		CCCompanyClearer=$15,
		CCCompanyIssuer=$16,
		credit_type=$17,
		CCExpDate=$18,
		CCNumber=$19,
		DebitCode=$20,
		DebitCurrency=$21,
		DebitTotal=$22,
		DebitType=$23,
		FirstPaymentTotal=$24,
		FixedPaymentTotal=$25,
		j_param=$26,
		TotalPayments=$27,
		TransactionInitTime=$28,
		TransactionUpdateTime=$29,
		VoucherID=$30,
		Ordkey=$31,
		updated_at=$32 
		WHERE id = $33`,
		p.Amount, p.PaymentStatus, p.PaymentType, p.OrderID, p.ParamX, p.AuthNo, p.ConfirmationKey, p.Success, p.PelecardToken,
		p.TransactionID, p.ErrorMsg, p.CardHebrewName, p.CCAbroadCard, p.CCBrand, p.CCCompanyClearer, p.CCCompanyIssuer, p.CreditType,
		p.CCExpDate, p.CCNumber, p.DebitCode, p.DebitCurrency, p.DebitTotal, p.DebitType, p.FirstPaymentTotal, p.FixedPaymentTotal,
		p.JParam, p.TotalPayments, p.TransactionInitTime, p.TransactionUpdateTime, p.VoucherID, p.Ordkey, time.Now(), p.ID)
	if err != nil {
		fmt.Errorf("problem updating payments: %w", err)
	}

	if updateRes.RowsAffected() != 1 {
		fmt.Println(updateRes.RowsAffected())
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not Saved"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, p)

}
