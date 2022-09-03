package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func getPaymentDetailById(ctx *gin.Context, id int) (PaymentDetails, error) {
	var (
		payDetail PaymentDetails
	)

	if err := DB.QueryRow(ctx, `SELECT 
			id,
			account_id,
			gateway_provider,
			cc_number,
			cc_expdate,
			active,
			created_at,
			updated_at,
			deleted_at from payment_details `+fmt.Sprintf("where id = %d", id)).Scan(
		&payDetail.ID,
		&payDetail.AccountID,
		&payDetail.GatewayProvider,
		&payDetail.CCNumber,
		&payDetail.CCExpDate,
		&payDetail.Active,
		&payDetail.CreatedAt,
		&payDetail.UpdatedAt,
		&payDetail.DeletedAt,
	); err != nil {
		return payDetail, err
	}
	return payDetail, nil

}
