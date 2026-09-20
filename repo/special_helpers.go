package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

func (o *OrdersDB) DeleteSpecialById(ctx context.Context, id int) error {
	// null.String, not string: specials.email is nullable, and handleCreateSpecial
	// binds repo.Special straight from the request with no email required, so
	// rows with a NULL email exist. pgx refuses NULL into *string, and this
	// function is on the revoke path — a row it cannot scan is a special that
	// cannot be removed through the API or by DeleteSpecialsByKeycloakId.
	var (
		email      null.String
		keycloakID null.String
		err        error
	)
	if err = o.QueryRow(ctx, `SELECT email, keycloak_id FROM specials where id=$1`, id).Scan(&email, &keycloakID); err != nil {
		return err
	}
	res, errUpdate := o.Exec(ctx, `UPDATE  specials SET end_date = now(), updated_at = now() WHERE  id = $1`, id)
	if errUpdate != nil {
		return fmt.Errorf("o.Exec: %w", errUpdate)
	}

	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	} else {
		// keycloak_id alongside the email: a keycloak-only special stores an
		// empty email, so email alone names nobody a consumer can revoke by —
		// and against an ilike predicate it names everyone.
		o.emitEvent(ctx, events.TypeDeleteSpecial, map[string]interface{}{"email": email.String, "keycloak_id": keycloakID.String})
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

// DeleteSpecialsByKeycloakId revokes the user's currently-active special(s) via
// DeleteSpecialById — the single primitive that ends a row and emits delete_special.
// Past spans keep their history and future spans stay scheduled.
func (o *OrdersDB) DeleteSpecialsByKeycloakId(ctx context.Context, keycloakID string) error {
	rows, err := o.Query(ctx,
		`SELECT id FROM specials WHERE keycloak_id=$1 AND start_date <= now() AND end_date > now()`,
		keycloakID)
	if err != nil {
		return fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("rows.Scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows.Err: %w", err)
	}

	for _, id := range ids {
		// ErrNoRowsAffected here means a concurrent revoke already ended the row.
		if err := o.DeleteSpecialById(ctx, id); err != nil && !errors.Is(err, common.ErrNoRowsAffected) {
			return fmt.Errorf("o.DeleteSpecialById [%d]: %w", id, err)
		}
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

// GetAllSpecials returns a page of specials, newest first.
//
// Bounded because the table only grows: revoking is a soft update that rewrites
// end_date, so nothing is ever removed, and the admin listing was reading every
// row ever granted on each request. The ORDER BY is what makes skip/limit mean
// anything — without it Postgres returns whatever the plan produces and two
// pages can overlap or miss rows.
func (o *OrdersDB) GetAllSpecials(ctx context.Context, skip, limit int) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials
		 ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, skip)
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

// GetSpecialsStartingBetween returns the specials whose window starts inside
// the given range, in no particular order.
//
// Both callers want a narrow band of start dates: the activator wants today,
// the importer's dedup index wants the span the sheet mentions. Reading the
// whole table for either grows without bound, since revoking is a soft update
// that rewrites end_date rather than removing the row.
func (o *OrdersDB) GetSpecialsStartingBetween(ctx context.Context, from, to time.Time) ([]*Special, error) {
	var specials []*Special
	rows, err := o.Query(ctx,
		`SELECT id, keycloak_id, email, start_date, end_date, category, subcategory FROM specials
		 WHERE start_date >= $1 AND start_date <= $2`, from, to)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		specials = append(specials, &spe)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
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
	rows, err := o.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory,created_at from specials where email ilike $1`, email)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var spe Special
		if err := rows.Scan(&spe.Id, &spe.KeycloakId, &spe.Email, &spe.StartDate, &spe.EndDate, &spe.Category, &spe.SubCategory, &spe.CreatedAt); err != nil {
			return nil, err
		}
		specials = append(specials, &spe)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return specials, nil

}
