package repo

import (
	"context"
	"fmt"
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"strings"
	"time"
)

func (o *OrdersDB) DeleteSpecialByEmail(ctx context.Context, email string) error {
	res, err := o.Exec(ctx, `Update  specials set end_date = now() WHERE  email = $1`, email)
	if err != nil {
		return err
	}

	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	} else {
		o.emitEvent(ctx, events.TypeDeleteSpecial, map[string]interface{}{"email": email})
	}

	return nil
}

func (o *OrdersDB) GetSpecialByEmail(ctx context.Context, email string) (*Special, error) {
	var spe Special

	if err := o.QueryRow(ctx, `SELECT id, keycloak_id, email, start_date, end_date, category, subcategory from specials where email = $1`, email).
		Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
		return nil, err
	}

	return &spe, nil
}

func (o *OrdersDB) GetSpecialByKeycloakID(ctx context.Context, keycloakID string) (*Special, error) {
	var spe Special
	if err := o.QueryRow(ctx, `SELECT id, keycloak_id, email, start_date, end_date, category, subcategory from specials where keycloak_id = $1`, keycloakID).
		Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
		return nil, err
	}
	return &spe, nil
}

func (o *OrdersDB) HasSpecialMembership(ctx context.Context, email string) (bool, error) {
	count, err := o.count(ctx, `select count(*) as total from specials where email = $1`, email)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (o *OrdersDB) CreateSpecial(ctx context.Context, s Special) (int, error) {
	createString, numString, createQueryArgs := prepareSpecialCreateQuery(s)
	if len(createQueryArgs) == 0 {
		return 0, common.ErrInvalidValues
	}

	var ID int
	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO specials (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&ID); err != nil {
		return 0, err
	}
	o.emitEvent(ctx, events.TypeCreateSpecial, map[string]interface{}{"keycloak_id": s.KeycloakId, "start_date": s.StartDate, "end_date": s.EndDate})
	return ID, nil
}

func prepareSpecialCreateQuery(req Special) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.KeycloakId.Valid {
		createStrings = append(createStrings, `"keycloak_id"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.KeycloakId.String)
	}
	if req.Email.Valid {
		createStrings = append(createStrings, `"email"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Email.String)
	}
	if req.StartDate.Valid {
		createStrings = append(createStrings, `"start_date"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.StartDate.Time)
	}
	if req.EndDate.Valid {
		createStrings = append(createStrings, `"end_date"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.EndDate.Time)
	}
	if req.Category.Valid {
		createStrings = append(createStrings, `"category"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Category.String)
	}

	if req.SubCategory.Valid {
		createStrings = append(createStrings, `"subcategory"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.SubCategory.String)
	}

	if len(args) != 0 {
		createStrings = append(createStrings, "created_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}
