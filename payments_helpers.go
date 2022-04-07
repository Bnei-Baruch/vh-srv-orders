package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/guregu/null.v4"
)

func getPaymentByEmail(ctx *gin.Context, email string) ([]PaymentByEmail, error) {

	paymentData := []PaymentByEmail{}

	rows, err := DB.Query(ctx, `select p.created_at, o."PaymentDate", o."Type", o."ProductType", o."Amount", o."Currency", p."CCNumber", p."ParamX", p."PaymentStatus"
	from payments as p, orders as o, accounts as a
	where a."Email" = $1
	and a.id = o."AccountID"
	and o.id = p."OrderID"
	order by o."PaymentDate" desc`, email)
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
