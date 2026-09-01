package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

const hhRequestColumns = `id, keycloak_id, type, requested_pct, months, note, status, rejection_note, created_at, updated_at`

func scanHHRequest(row pgx.Row) (*HHRequest, error) {
	var r HHRequest
	err := row.Scan(&r.ID, &r.KeycloakID, &r.Type, &r.RequestedPct, &r.Months, &r.Note,
		&r.Status, &r.RejectionNote, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateHHRequest deletes any pending request for the member, then inserts the new one,
// mirroring the v1 profiles request flow. Both operations run in a transaction.
func (o *OrdersDB) CreateHHRequest(ctx context.Context, req HHRequestReq) (*HHRequest, error) {
	tx, err := o.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("o.Begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM hh_requests WHERE keycloak_id = $1 AND status = $2`,
		req.KeycloakID, common.HHRequestStatusRequested); err != nil {
		return nil, fmt.Errorf("tx.Exec: %w", err)
	}

	request, err := scanHHRequest(tx.QueryRow(ctx,
		`INSERT INTO hh_requests (keycloak_id, type, requested_pct, months, note)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+hhRequestColumns,
		req.KeycloakID, req.Type, req.RequestedPct, req.Months, req.Note))
	if err != nil {
		return nil, fmt.Errorf("tx.QueryRow.Scan: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("tx.Commit: %w", err)
	}

	o.emitEvent(ctx, events.TypeHHRequestCreated,
		map[string]interface{}{"request_id": request.ID, "keycloak_id": request.KeycloakID})
	return request, nil
}

// GetAllHHRequests returns requests joined with the grant each produced (if any),
// optionally filtered by status and/or exact keycloak_id, newest first.
func (o *OrdersDB) GetAllHHRequests(ctx context.Context, status, keycloakID string) ([]*HHRequestWithGrant, error) {
	query := `SELECT r.id, r.keycloak_id, r.type, r.requested_pct, r.months, r.note,
	                 r.status, r.rejection_note, r.created_at, r.updated_at,
	                 g.id, g.request_id, g.keycloak_id, g.type, g.discount_pct,
	                 g.start_date, g.end_date, g.updated_at, g.note,
	                 a."FirstName", a."LastName"
	          FROM hh_requests r
	          LEFT JOIN hh_grants g ON g.request_id = r.id
	          LEFT JOIN LATERAL (
	              SELECT "FirstName", "LastName" FROM accounts
	              WHERE "UserKey" = r.keycloak_id LIMIT 1
	          ) a ON true`
	var conditions []string
	var args []interface{}
	if status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if keycloakID != "" {
		args = append(args, keycloakID)
		conditions = append(conditions, fmt.Sprintf("r.keycloak_id = $%d", len(args)))
	}
	for i, c := range conditions {
		if i == 0 {
			query += " WHERE " + c
		} else {
			query += " AND " + c
		}
	}
	query += ` ORDER BY r.id DESC`

	rows, err := o.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("o.Query: %w", err)
	}
	defer rows.Close()

	var result []*HHRequestWithGrant
	for rows.Next() {
		var r HHRequestWithGrant
		var g HHGrant
		var gID, gRequestID, gPct null.Int
		var gKcid, gType null.String
		var gStart, gEnd, gUpdated null.Time
		var firstName, lastName null.String
		if err := rows.Scan(
			&r.ID, &r.KeycloakID, &r.Type, &r.RequestedPct, &r.Months, &r.Note,
			&r.Status, &r.RejectionNote, &r.CreatedAt, &r.UpdatedAt,
			&gID, &gRequestID, &gKcid, &gType, &gPct,
			&gStart, &gEnd, &gUpdated, &g.Note,
			&firstName, &lastName,
		); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		r.MemberName = strings.TrimSpace(firstName.String + " " + lastName.String)
		if gID.Valid {
			g.ID = gID.Int
			g.RequestID = gRequestID.Int
			g.KeycloakID = gKcid.String
			g.Type = gType.String
			g.DiscountPct = gPct.Int
			g.StartDate = gStart.Time
			g.EndDate = gEnd.Time
			g.UpdatedAt = gUpdated.Time
			r.Grant = &g
		}
		result = append(result, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}
	return result, nil
}

// ConcludeHHRequest approves or denies a pending request. On approval it ends any
// active grant for the member and creates the new grant in the same transaction.
// Returns ErrNoRowsAffected if the request does not exist or is already concluded.
func (o *OrdersDB) ConcludeHHRequest(ctx context.Context, id int, c HHRequestConclusion) (*HHRequest, error) {
	status := common.HHRequestStatusDenied
	if c.Approved {
		status = common.HHRequestStatusApproved
	}

	tx, err := o.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("o.Begin: %w", err)
	}
	defer tx.Rollback(ctx)

	request, err := scanHHRequest(tx.QueryRow(ctx,
		`UPDATE hh_requests
		 SET status = $2, rejection_note = $3, updated_at = NOW()
		 WHERE id = $1 AND status = $4
		 RETURNING `+hhRequestColumns,
		id, status, c.RejectionNote, common.HHRequestStatusRequested))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, common.ErrNoRowsAffected
		}
		return nil, fmt.Errorf("tx.QueryRow.Scan: %w", err)
	}

	if c.Approved {
		if _, err := tx.Exec(ctx,
			`UPDATE hh_grants SET end_date = NOW() - INTERVAL '1 day', updated_at = NOW()
			 WHERE keycloak_id = $1 AND end_date > NOW() AND start_date <= NOW()`,
			request.KeycloakID); err != nil {
			return nil, fmt.Errorf("tx.Exec: %w", err)
		}
		start := time.Now()
		if c.StartDate.Valid {
			start = c.StartDate.Time
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO hh_grants (request_id, keycloak_id, type, discount_pct, start_date, end_date, note)
			 VALUES ($1, $2, $3, $4, $5, $5::timestamptz + make_interval(months => $6), $7)`,
			request.ID, request.KeycloakID, c.Type, c.DiscountPct, start, c.Months, c.Note); err != nil {
			return nil, fmt.Errorf("tx.Exec (grant): %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("tx.Commit: %w", err)
	}

	o.emitEvent(ctx, events.TypeHHRequestConcluded,
		map[string]interface{}{"request_id": request.ID, "keycloak_id": request.KeycloakID, "approved": c.Approved})
	return request, nil
}
