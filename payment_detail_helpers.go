package main

import (
	"fmt"
	"strings"
	"time"

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

func createPaymentDetailsById(ctx *gin.Context, p PaymentDetails) (int, error) {

	createString, numString, createQueryArgs := preparePaymentDetailsCreateQuery(p)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := DB.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payment_details (%s) VALUES (%s) RETURNING id`, createString, numString),
			createQueryArgs...).Scan(
			&ID,
		); err != nil {
			return 0, err
		}
		return ID, nil
	} else {
		return 0, fmt.Errorf("invalid body")
	}

}

func softDeletePaymentDetailById(c *gin.Context, id int) error {
	_, err := DB.Exec(c, "UPDATE payment_details SET deleted_at = $1 WHERE id = $2", time.Now(), id)
	return err
}

func patchPaymentDetailsById(c *gin.Context, req PaymentDetails, id int) error {

	toUpdate, toUpdateArgs := preparePaymentDetailsUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE payment_details SET %s WHERE id=%d`, toUpdate, id),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating payment_details: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("payment_details not updated as no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func preparePaymentDetailsUpdateQuery(req PaymentDetails) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.AccountID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"account_id"=$%d`, len(updateStrings)+1))
		args = append(args, req.AccountID.Int64)
	}
	if req.GatewayProvider.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"gateway_provider"=$%d`, len(updateStrings)+1))
		args = append(args, req.GatewayProvider.String)
	}
	if req.CCNumber.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"cc_number"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.CCExpDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"cc_expdate"=$%d`, len(updateStrings)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.Active.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"active"=$%d`, len(updateStrings)+1))
		args = append(args, req.Active.Bool)
	}
	if req.CreatedAt.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"created_at"=$%d`, len(updateStrings)+1))
		args = append(args, req.CreatedAt.Time)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func preparePaymentDetailsCreateQuery(req PaymentDetails) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.AccountID.Valid {
		createStrings = append(createStrings, `"account_id"`)
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.AccountID.Int64)
	}

	if req.GatewayProvider.Valid {
		createStrings = append(createStrings, `"gateway_provider"`)
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.GatewayProvider.String)
	}

	if req.CCNumber.Valid {
		createStrings = append(createStrings, `"cc_number"`)
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.CCNumber.String)
	}

	if req.CCExpDate.Valid {
		createStrings = append(createStrings, `"cc_expdate"`)
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.CCExpDate.String)
	}

	if req.Active.Valid {
		createStrings = append(createStrings, `"active"`)
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.Active.Bool)
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
