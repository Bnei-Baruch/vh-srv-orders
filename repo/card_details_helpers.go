package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func (o *OrdersDB) GetCardDetailById(ctx context.Context, id int) (*CardDetails, error) {
	var card CardDetails

	if err := o.QueryRow(ctx, `SELECT 
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
		&card.ID,
		&card.AccountID,
		&card.GatewayProvider,
		&card.CCNumber,
		&card.CCExpDate,
		&card.Active,
		&card.Token,
		&card.CreatedAt,
		&card.UpdatedAt,
		&card.DeletedAt,
	); err != nil {
		return nil, err
	}

	return &card, nil
}

func (o *OrdersDB) CreateCardDetailsAndGetId(ctx context.Context, p CardDetails) (int, error) {
	createString, numString, createQueryArgs := prepareCardDetailsCreateQuery(p)
	if len(createQueryArgs) == 0 {
		return 0, common.ErrInvalidValues
	}

	var ID int
	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO card_details (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&ID); err != nil {
		return 0, err
	}

	return ID, nil
}

func (o *OrdersDB) SoftDeleteCardDetailById(ctx context.Context, id int) error {
	_, err := o.Exec(ctx, "UPDATE card_details SET deleted_at = $1 WHERE id = $2", time.Now(), id)
	return err
}

func (o *OrdersDB) PatchCardDetailsById(ctx context.Context, req CardDetails, id int) error {
	toUpdate, toUpdateArgs := prepareCardDetailsUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE card_details SET %s WHERE id=%d`, toUpdate, id), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	return nil
}

func (o *OrdersDB) GetAllCardDetails(ctx context.Context, skip int, limit int) ([]CardDetails, error) {
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery := buildAndGetCardDetailsWhereQuery()

	rows, err := o.Query(ctx, `
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
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	cardDetails := []CardDetails{}
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
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		cardDetails = append(cardDetails, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}

	return cardDetails, rows.Err()
}

//func addCardDetailsFromAllExistingOrders(ctx *gin.Context, orderType string) {
//
//	var payments *[]Payment
//	var err error
//	var timeNow = time.Now()
//
//	var terminalNumber string
//
//	payments, err = main.GetAllPayments(ctx, int(0), int(100), "", &timeNow, "", "", orderType, "", int(0), "true", int(0), "")
//	if err != nil {
//		fmt.Println("error getting payments :: ", err.Error())
//		return
//	}
//
//	if orderType == "recurring" {
//		terminalNumber = "2814722016"
//	} else {
//		terminalNumber = "5776492014"
//	}
//
//	// loop over allPayments
//	for _, payment := range *payments {
//		var pelecardCardDetail utils.PelecardCardDetail
//		var peleErr error
//		pelecardCardDetail, peleErr = utils.FetchPelecardCardDetailFromToken(payment.PelecardToken.String, terminalNumber)
//
//		if peleErr != nil {
//			fmt.Println("error fetching pelecard card detail")
//			return
//		}
//
//		if pelecardCardDetail.ResultData.CreditCardNumber != "" && pelecardCardDetail.ResultData.ExpirationDate != "" {
//			order := main.getOrderByID(ctx, uint(payment.OrderID.Int64))
//
//			var cardDetails CardDetails
//			cardDetails.AccountID = order.AccountID
//			cardDetails.GatewayProvider = null.NewString("pelecard", true)
//			cardDetails.Token = null.NewString(payment.PelecardToken.String, true)
//			first4Num := pelecardCardDetail.ResultData.CreditCardNumber[0:4]
//			last4 := pelecardCardDetail.ResultData.CreditCardNumber[len(pelecardCardDetail.ResultData.CreditCardNumber)-4:]
//			censoredCreditCardNum := first4Num + "****" + last4
//
//			cardDetails.CCNumber = null.NewString(censoredCreditCardNum, true)
//			cardDetails.CCExpDate = null.NewString(pelecardCardDetail.ResultData.ExpirationDate, true)
//			cardDetails.Active = null.NewBool(true, true)
//
//			_, err = createCardDetailsAndGetId(ctx, cardDetails)
//			// Error can originate from duplicate entry in DB for same payment details for same account id
//			if err != nil {
//				fmt.Println("error creating payment details")
//				fmt.Println(err.Error())
//				fmt.Println("--------------------------------")
//			}
//		}
//	}
//}

func prepareCardDetailsUpdateQuery(req CardDetails) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.AccountID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf("account_id=$%d", len(updateStrings)+1))
		args = append(args, req.AccountID.Int)
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
		args = append(args, req.AccountID.Int)
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

func buildAndGetCardDetailsWhereQuery() (string, string) {
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

	return whereString.String(), orderBy.String()
}
