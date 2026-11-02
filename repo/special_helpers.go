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
	// null.String, not string: specials.email is nullable and rows from before
	// #53 may hold NULL. pgx refuses NULL into *string, and a scan error here
	// would hide the real reason the revoke is refused below.
	var (
		email      null.String
		keycloakID null.String
		err        error
	)
	if err = o.pool.QueryRow(ctx, `SELECT email, keycloak_id FROM specials where id=$1`, id).Scan(&email, &keycloakID); err != nil {
		return err
	}
	// vh-srv-profile revokes by email only, so ending a special that has none
	// would leave the membership active there. Refuse, and leave the row as is.
	// '' as well as NULL: older rows stored the empty string for "absent".
	if email.String == "" {
		return fmt.Errorf("special %d has no email to revoke by: %w", id, common.ErrInvalidValues)
	}
	res, errUpdate := o.pool.Exec(ctx, `UPDATE  specials SET end_date = now(), updated_at = now() WHERE  id = $1`, id)
	if errUpdate != nil {
		return fmt.Errorf("o.pool.Exec: %w", errUpdate)
	}

	if res.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	} else {
		// keycloak_id alongside the email, so a consumer is not left with only
		// an address the account may have changed since.
		o.emitEvent(ctx, events.TypeDeleteSpecial, map[string]interface{}{"email": email.String, "keycloak_id": keycloakID.String})
	}
	return nil
}

func (o *OrdersDB) SetKeycloakIdByEmail(ctx context.Context, email string, keycloakID string) error {
	if email == "" || keycloakID == "" {
		return fmt.Errorf("%w: email and keycloak_id are required", common.ErrInvalidValues)
	}
	_, err := o.pool.Exec(ctx, `UPDATE specials SET keycloak_id = $1, updated_at=now() WHERE email=$2`, keycloakID, email)
	if err != nil {
		return fmt.Errorf("SetKeycloakIdByEmail: %w", err)
	}
	return nil
}

// DeleteSpecialsByKeycloakId revokes the user's currently-active special(s) via
// DeleteSpecialById — the single primitive that ends a row and emits delete_special.
// Past spans keep their history and future spans stay scheduled.
func (o *OrdersDB) DeleteSpecialsByKeycloakId(ctx context.Context, keycloakID string) error {
	rows, err := o.pool.Query(ctx,
		`SELECT id FROM specials WHERE keycloak_id=$1 AND start_date <= now() AND end_date > now()`,
		keycloakID)
	if err != nil {
		return fmt.Errorf("o.pool.Query: %w", err)
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
	rows, err := o.pool.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials where id = $1`, id)

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
	rows, err := o.pool.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials where keycloak_id = $1`, keycloakID)

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
	rows, err := o.pool.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory from specials
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
	rows, err := o.pool.Query(ctx,
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

// CountSpecials is how a caller tells a full page from the whole table. The
// listing response carries no other marker, so without it a truncated answer
// and a complete one are the same JSON.
func (o *OrdersDB) CountSpecials(ctx context.Context) (int, error) {
	total, err := o.count(ctx, `SELECT count(*) FROM specials`)
	if err != nil {
		return 0, fmt.Errorf("o.count: %w", err)
	}
	return int(total), nil
}

func (o *OrdersDB) HasSpecialMembership(ctx context.Context, email string) (bool, error) {
	count, err := o.count(ctx, `select count(*) as total from specials where email = $1`, email)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (o *OrdersDB) CreateSpecial(ctx context.Context, s Special) (int, error) {
	// An email is required. A row with neither identifier can be reached by
	// nothing but its id (the specials_has_identifier CHECK refuses it too),
	// and a keycloak-only row could be granted but never revoked:
	// DeleteSpecialById refuses it, because vh-srv-profile revokes by email.
	if !s.Email.Valid || s.Email.String == "" {
		return 0, fmt.Errorf("special needs an email: %w", common.ErrInvalidValues)
	}
	createString, numString, createQueryArgs := prepareSpecialCreateQuery(s)

	var ID int
	if err := o.pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO specials (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&ID); err != nil {
		return 0, err
	}
	// As in delete_special: vh-srv-profile reads both identifiers with .(string),
	// so an absent one goes out as "", never null.
	o.emitEvent(ctx, events.TypeCreateSpecial,
		map[string]interface{}{"email": s.Email.String, "keycloak_id": s.KeycloakId.String, "start_date": s.StartDate, "end_date": s.EndDate})
	return ID, nil
}

func prepareSpecialCreateQuery(req Special) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.KeycloakId.Valid && req.KeycloakId.String != "" {
		createStrings = append(createStrings, `"keycloak_id"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.KeycloakId.String)
	}
	if req.Email.Valid && req.Email.String != "" {
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

// GetAllSpecialsByEmail finds specials by the address they were granted to.
//
// A special the sheet supplied no email for stores NULL there, and `ilike` is
// UNKNOWN against NULL, so those rows are not returned — not even for a
// wildcard. That is the intended answer to "which specials belong to this
// address": a keycloak-only grant belongs to no address, and it is reachable
// through GetSpecialsByKeycloakId. It is worth stating because the rows used to
// carry an empty string, which a wildcard did match.
func (o *OrdersDB) GetAllSpecialsByEmail(ctx context.Context, email string) ([]*Special, error) {
	var specials []*Special
	rows, err := o.pool.Query(ctx, `SELECT id, keycloak_id, email, start_date,end_date,category,subcategory,created_at from specials where email ilike $1`, email)

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
