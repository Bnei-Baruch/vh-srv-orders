package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

func (o *OrdersDB) GetCardDetailById(ctx context.Context, id int) (*CardDetails, error) {
	var card CardDetails

	if err := o.pool.QueryRow(ctx, `SELECT 
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
	if err := o.pool.QueryRow(ctx, `INSERT INTO card_details (`+createString+`) VALUES (`+numString+`) RETURNING id`,
		createQueryArgs...).Scan(&ID); err != nil {
		return 0, err
	}

	return ID, nil
}

func (o *OrdersDB) SoftDeleteCardDetailById(ctx context.Context, id int) error {
	_, err := o.pool.Exec(ctx, "UPDATE card_details SET deleted_at = $1 WHERE id = $2", time.Now(), id)
	return err
}

func (o *OrdersDB) PatchCardDetailsById(ctx context.Context, req CardDetails, id int) error {
	toUpdate, toUpdateArgs := prepareCardDetailsUpdateQuery(req)
	if len(toUpdateArgs) == 0 {
		return common.ErrInvalidValues
	}

	updateRes, err := o.pool.Exec(ctx, fmt.Sprintf(`UPDATE card_details SET %s WHERE id=%d`, toUpdate, id), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}
	if updateRes.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}

	return nil
}

func (o *OrdersDB) GetAllCardDetails(ctx context.Context, skip int, limit int) ([]CardDetails, error) {
	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)
	whereQuery, orderByQuery := buildAndGetCardDetailsWhereQuery()

	rows, err := o.pool.Query(ctx, `
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
		return nil, fmt.Errorf("o.pool.Query: %w", err)
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

func prepareCardDetailsUpdateQuery(req CardDetails) (string, queryArgs) {
	var c colVals

	if req.AccountID.Valid {
		c.add("account_id", req.AccountID.Int)
	}
	if req.GatewayProvider.Valid {
		c.add("gateway_provider", req.GatewayProvider.String)
	}
	if req.CCNumber.Valid {
		c.add("cc_number", req.CCNumber.String)
	}
	if req.CCExpDate.Valid {
		c.add("cc_expdate", req.CCExpDate.String)
	}
	if req.Active.Valid {
		c.add("active", req.Active.Bool)
	}
	if req.Token.Valid {
		c.add("token", req.Token.String)
	}
	if req.CreatedAt.Valid {
		c.add("created_at", req.CreatedAt.Time)
	}

	if !c.empty() {
		c.add("updated_at", time.Now())
	}

	return c.setList()
}

func prepareCardDetailsCreateQuery(req CardDetails) (string, string, queryArgs) {
	var c colVals

	if req.AccountID.Valid {
		c.add("account_id", req.AccountID.Int)
	}

	if req.GatewayProvider.Valid {
		c.add("gateway_provider", req.GatewayProvider.String)
	}

	if req.CCNumber.Valid {
		c.add("cc_number", req.CCNumber.String)
	}

	if req.CCExpDate.Valid {
		c.add("cc_expdate", req.CCExpDate.String)
	}

	if req.Active.Valid {
		c.add("active", req.Active.Bool)
	}

	if req.Token.Valid {
		c.add("token", req.Token.String)
	}

	if !c.empty() {
		c.add("created_at", time.Now())
		c.add("updated_at", time.Now())
	}

	return c.insertLists()
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
