package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

func (o *OrdersDB) DeleteSpecialById(ctx context.Context, id int) error {
	var (
		email string
		err   error
	)
	if err = o.QueryRow(ctx, `SELECT email FROM specials where id=$1`, id).Scan(&email); err != nil {
		return err
	}
	res, errUpdate := o.Exec(ctx, `UPDATE  specials SET end_date = now(), updated_at = now() WHERE  id = $1`, id)
	if errUpdate != nil {
		return err
	}

	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	} else {
		o.emitEvent(ctx, events.TypeDeleteSpecial, map[string]interface{}{"email": email})
	}
	return nil
}

func (o *OrdersDB) SetKeycloakIdByEmail(ctx context.Context, email string, keycloakID string) error {
	_, err := o.Exec(ctx, `UPDATE specials SET keycloak_id = $1, updated_at=now() WHERE email=$2`, keycloakID, email)
	if err != nil {
		return fmt.Errorf("SetKeycloakIdByEmail: %w", err)
	}
	return nil
}
func (o *OrdersDB) DeleteSpecialsByKeycloakId(ctx context.Context, keycloakID string) error {
	_, err := o.Exec(ctx, `UPDATE specials SET end_date = now(),  updated_at = now() WHERE keycloak_id=$1`, keycloakID)
	if err != nil {
		return fmt.Errorf("DeleteSpecialsByKeycloakId: %w", err)
	}
	return nil
}

func (o *OrdersDB) GetSpecialsById(ctx context.Context, id string) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials where id = $1`, id)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
			return nil, err
		}
		specials = append(specials, &spe)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return specials, nil
}

func (o *OrdersDB) GetSpecialsByKeycloakId(ctx context.Context, keycloakID string) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials where keycloak_id = $1`, keycloakID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
			return nil, err
		}
		specials = append(specials, &spe)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return specials, nil
}

func (o *OrdersDB) GetAllSpecials(ctx context.Context) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials`)
	if err != nil {
		return specials, err
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
			return specials, err
		}
		specials = append(specials, &spe)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return specials, nil
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
	o.emitEvent(ctx, events.TypeCreateSpecial,
		map[string]interface{}{"email": s.Email, "keycloak_id": s.KeycloakId, "start_date": s.StartDate, "end_date": s.EndDate})
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

func (o *OrdersDB) GetAllSpecialsByEmail(ctx context.Context, email string) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials where email = $1`, email)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
			return nil, err
		}
		specials = append(specials, &spe)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return specials, nil

}

func (o *OrdersDB) GetUniqueEmailsFromSpecial(ctx context.Context) ([]string, error) {
	var emails []string
	rows, err := o.Query(ctx, `SELECT DISTINCT specials.email from specials`)

	if err != nil {
		return emails, err
	}
	defer rows.Close()

	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return emails, nil
}
