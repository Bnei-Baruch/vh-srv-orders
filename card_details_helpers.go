package main

import (
	"fmt"
	"orderservices/orders/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/guregu/null.v4"
)

func getCardDetailById(ctx *gin.Context, id int) (CardDetails, error) {
	var (
		payDetail CardDetails
	)

	if err := DB.QueryRow(ctx, `SELECT 
			id,
			account_id,
			gateway_provider,
			cc_number,
			cc_expdate,
			active,
			token,
			created_at,
			updated_at,
			deleted_at from card_details `+fmt.Sprintf("where id = %d", id)).Scan(
		&payDetail.ID,
		&payDetail.AccountID,
		&payDetail.GatewayProvider,
		&payDetail.CCNumber,
		&payDetail.CCExpDate,
		&payDetail.Active,
		&payDetail.Token,
		&payDetail.CreatedAt,
		&payDetail.UpdatedAt,
		&payDetail.DeletedAt,
	); err != nil {
		return payDetail, err
	}
	return payDetail, nil

}

func createCardDetailsAndGetId(ctx *gin.Context, p CardDetails) (int, error) {

	createString, numString, createQueryArgs := prepareCardDetailsCreateQuery(p)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := DB.QueryRow(ctx, fmt.Sprintf(`INSERT INTO card_details (%s) VALUES (%s) RETURNING id`, createString, numString),
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

func softDeleteCardDetailById(c *gin.Context, id int) error {
	_, err := DB.Exec(c, "UPDATE card_details SET deleted_at = $1 WHERE id = $2", time.Now(), id)
	return err
}

func patchCardDetailsById(c *gin.Context, req CardDetails, id int) error {

	toUpdate, toUpdateArgs := prepareCardDetailsUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE card_details SET %s WHERE id=%d`, toUpdate, id),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating card_details: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("card_details not updated as no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func GetAllCardDetails(ctx *gin.Context, skip int, limit int) (*[]CardDetails, error) {

	cardDetails := []CardDetails{}

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)

	whereQuery, orderByQuery, queryBuildErr := buildAndGetCardDetailsWhereQuery()

	if queryBuildErr != nil {
		return &cardDetails, queryBuildErr
	}

	rows, err := DB.Query(ctx, `
		SELECT 
			id,
			account_id,
			gateway_provider,
			cc_number,
			cc_expdate,
			active,
			token,
			created_at,
			updated_at,
			deleted_at from card_details`+whereQuery+orderByQuery+limitOffsetString)
	if err != nil {
		fmt.Println("--error-while-executing-query", err)
		return &cardDetails, err
	}
	defer rows.Close()
	for rows.Next() {
		var d CardDetails
		err := rows.Scan(
			&d.ID,
			&d.AccountID,
			&d.GatewayProvider,
			&d.CCNumber,
			&d.CCExpDate,
			&d.Active,
			&d.Token,
			&d.CreatedAt,
			&d.UpdatedAt,
			&d.DeletedAt,
		)
		if err != nil {
			return &cardDetails, err
		}
		cardDetails = append(cardDetails, d)
	}
	return &cardDetails, rows.Err()

}

func addCardDetailsFromAllExistingOrders(ctx *gin.Context, orderType string) {

	var payments *[]Payment
	var err error
	var timeNow = time.Now()

	var terminalNumber string

	payments, err = GetAllPayments(ctx, int(0), int(100), "", &timeNow, "", "", orderType, "", int(0), "true")
	if err != nil {
		fmt.Println("error getting payments :: ", err.Error())
		return
	}

	if orderType == "recurring" {
		terminalNumber = "2814722016"
	} else {
		terminalNumber = "5776492014"
	}

	// loop over allPayments
	for _, payment := range *payments {
		var pelecardCardDetail utils.PelecardCardDetail
		var peleErr error
		pelecardCardDetail, peleErr = utils.FetchPelecardCardDetailFromToken(payment.PelecardToken.String, terminalNumber)

		if peleErr != nil {
			fmt.Println("error fetching pelecard card detail")
			return
		}

		if pelecardCardDetail.ResultData.CreditCardNumber != "" && pelecardCardDetail.ResultData.ExpirationDate != "" {
			order := getOrderByID(ctx, uint(payment.OrderID.Int64))

			var cardDetails CardDetails
			cardDetails.AccountID = order.AccountID
			cardDetails.GatewayProvider = null.NewString("pelecard", true)
			cardDetails.Token = null.NewString(payment.PelecardToken.String, true)
			first4Num := pelecardCardDetail.ResultData.CreditCardNumber[0:4]
			last4 := pelecardCardDetail.ResultData.CreditCardNumber[len(pelecardCardDetail.ResultData.CreditCardNumber)-4:]
			censoredCreditCardNum := first4Num + "****" + last4

			cardDetails.CCNumber = null.NewString(censoredCreditCardNum, true)
			cardDetails.CCExpDate = null.NewString(pelecardCardDetail.ResultData.ExpirationDate, true)
			cardDetails.Active = null.NewBool(true, true)

			_, err = createCardDetailsAndGetId(ctx, cardDetails)
			// Error can originate from duplicate entry in DB for same payment details for same account id
			if err != nil {
				fmt.Println("error creating payment details")
				fmt.Println(err.Error())
				fmt.Println("--------------------------------")
			}
		}
	}
}

func prepareCardDetailsUpdateQuery(req CardDetails) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.AccountID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("account_id=$%d", len(updateStrings)+1))
		args = append(args, req.AccountID.Int64)
	}
	if req.GatewayProvider.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("gateway_provider=$%d", len(updateStrings)+1))
		args = append(args, req.GatewayProvider.String)
	}
	if req.CCNumber.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_number=$%d", len(updateStrings)+1))
		args = append(args, req.CCNumber.String)
	}
	if req.CCExpDate.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("cc_expdate=$%d", len(updateStrings)+1))
		args = append(args, req.CCExpDate.String)
	}
	if req.Active.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("active=$%d", len(updateStrings)+1))
		args = append(args, req.Active.Bool)
	}
	if req.Token.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("token=$%d", len(updateStrings)+1))
		args = append(args, req.Token.String)
	}
	if req.CreatedAt.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("created_at=$%d", len(updateStrings)+1))
		args = append(args, req.CreatedAt.Time)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}

func prepareCardDetailsCreateQuery(req CardDetails) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.AccountID.Valid {
		createStrings = append(createStrings, "account_id")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.AccountID.Int64)
	}

	if req.GatewayProvider.Valid {
		createStrings = append(createStrings, "gateway_provider")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.GatewayProvider.String)
	}

	if req.CCNumber.Valid {
		createStrings = append(createStrings, "cc_number")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.CCNumber.String)
	}

	if req.CCExpDate.Valid {
		createStrings = append(createStrings, "cc_expdate")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.CCExpDate.String)
	}

	if req.Active.Valid {
		createStrings = append(createStrings, "active")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.Active.Bool)
	}

	if req.Token.Valid {
		createStrings = append(createStrings, "token")
		numString = append(numString, fmt.Sprintf(`$%d`, len(numString)+1))
		args = append(args, req.Token.String)
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

func buildAndGetCardDetailsWhereQuery() (string, string, error) {

	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	orderBy.WriteString(fmt.Sprintf(" ORDER BY updated_at %s", "desc"))

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}
