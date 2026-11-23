package repo

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// RenewalData holds all DB data needed to process an order renewal.
// Loaded once via LoadRenewalData, then used by the billing domain to
// resolve pricing, build the charge request, and execute the payment.
type RenewalData struct {
	Order       *Order
	Account     *Account
	PrevPayment *Payment     // previous payment (carries token/auth for reuse)
	Card        *CardDetails // resolved active card, nil if order has no card_details_id
}

// LoadRenewalData loads all data needed to renew an order.
// Returns ErrPrePayment-wrapped errors for any lookup failure.
func (o *OrdersDB) LoadRenewalData(ctx context.Context, orderID uint) (*RenewalData, error) {
	order, err := o.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: o.GetOrderByID: %w", common.ErrPrePayment, err)
	}

	prevPayment, err := o.getPaymentForOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: o.getPaymentForOrderID: %w", common.ErrPrePayment, err)
	}

	account, err := o.GetAccountForOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: o.GetAccountForOrderID: %w", common.ErrPrePayment, err)
	}

	var card *CardDetails
	if !order.CardDetailsId.IsZero() {
		cd, err := o.GetCardDetailById(ctx, order.CardDetailsId.Int)
		if err != nil {
			return nil, fmt.Errorf("%w: o.GetCardDetailById: %w", common.ErrPrePayment, err)
		}
		if !cd.Active.Valid || !cd.Active.Bool {
			return nil, fmt.Errorf("%w: inactive card [%d]", common.ErrPrePayment, order.CardDetailsId.Int)
		}
		if !cd.Token.Valid || cd.Token.String == "" {
			return nil, fmt.Errorf("%w: empty token [%d]", common.ErrPrePayment, order.CardDetailsId.Int)
		}
		card = cd
	}

	return &RenewalData{
		Order:       order,
		Account:     account,
		PrevPayment: prevPayment,
		Card:        card,
	}, nil
}

// CreateRenewalPayment creates a pending payment record for a renewal with the resolved price.
// Suppresses TypeCreatePayment event — TypeUpdateOrder will be emitted by the billing domain.
func (o *OrdersDB) CreateRenewalPayment(ctx context.Context, data *RenewalData, amount float64, currency, pricingVersion string, pricingEvaluation null.JSON, pmx string) (*Payment, error) {
	p := Payment{
		Amount:            null.Float64From(amount),
		Currency:          null.StringFrom(currency),
		PaymentType:       null.StringFrom(common.PaymentTypePelecard),
		OrderID:           null.IntFrom(data.Order.ID),
		PaymentStatus:     null.StringFrom(common.PaymentStatusPending),
		PricingVersion:    null.StringFrom(pricingVersion),
		PricingEvaluation: pricingEvaluation,
	}

	createString, numString, createQueryArgs := preparePaymentCreateQuery(p)
	if err := o.pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&p.ID); err != nil {
		return nil, fmt.Errorf("o.pool.QueryRow.Scan [insert payment]: %w", err)
	}

	createPelecardString, numPelecardString, createPelecardQueryArgs := preparePelecardPaymentCreateQuery(p, p.ID)
	_, err := o.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO payments_pelecard (%s) VALUES (%s)`, createPelecardString, numPelecardString),
		createPelecardQueryArgs...)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Exec [insert pelecard]: %w", err)
	}

	paramx := "m-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX") + pmx
	ordkey := "ord-" + strconv.FormatUint(uint64(data.Order.ID), 10) + os.Getenv("SUFX")
	p.ParamX = null.StringFrom(paramx)
	p.Ordkey = null.StringFrom(ordkey)

	// Copy token/auth from previous payment or active card
	p.AuthNo = data.PrevPayment.AuthNo
	p.PelecardToken = data.PrevPayment.PelecardToken
	if data.Card != nil {
		p.PelecardToken = data.Card.Token
		p.CCNumber = data.Card.CCNumber
		p.CCExpDate = data.Card.CCExpDate
	}

	if err := updatePaymentRow(ctx, o.pool, p, p.ID); err != nil {
		return nil, fmt.Errorf("updatePaymentRow: %w", err)
	}

	return &p, nil
}

// FinalizeRenewal persists the gateway result in a single transaction.
// Updates payment status, order flag/status, and pelecard_payment record.
// Does NOT emit events — the billing domain is responsible for that.
func (o *OrdersDB) FinalizeRenewal(ctx context.Context, orderID uint, payment *Payment) error {
	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: o.pool.Begin: %w", common.ErrPostPayment, err)
	}
	defer tx.Rollback(ctx)

	if payment.Success.String == "1" {
		if _, err := tx.Exec(ctx, `UPDATE orders SET "Flag"=$1, updated_at=$2 WHERE id = $3`,
			common.OrderFlagRenewed, time.Now(), orderID); err != nil {
			return fmt.Errorf("%w: tx.Exec [flag renewed]: %w", common.ErrPostPayment, err)
		}
	}

	if err := writePaymentFacts(ctx, tx, *payment, payment.Amount, payment.Ordkey, payment.ID); err != nil {
		return fmt.Errorf("%w: writePaymentFacts: %w", common.ErrPostPayment, err)
	}

	now := time.Now()
	if payment.Success.String == "1" {
		_, err = tx.Exec(ctx, `UPDATE orders SET "Status"=$1, "PaymentDate"=$2, updated_at=$3 WHERE id = $4`,
			common.OrderStatusPaid, now, now, payment.OrderID.Int)
	} else {
		// A charge_pending order settled as not charged goes back to torenew, so
		// a re-run of billing tries it again like any other declined order.
		_, err = tx.Exec(ctx, `UPDATE orders SET "Status"=$1,
				"Flag" = CASE WHEN "Flag" = $2 THEN $3 ELSE "Flag" END,
				updated_at=$4 WHERE id = $5`,
			common.OrderStatusNoSuccess, common.OrderFlagChargePending, common.OrderFlagToRenew, now, payment.OrderID.Int)
	}
	if err != nil {
		return fmt.Errorf("%w: tx.Exec [update order status]: %w", common.ErrPostPayment, err)
	}

	return tx.Commit(ctx)
}

// MarkRenewalChargePending takes an order out of torenew after a charge whose
// outcome is unknown. Its payment stays pending, untouched, for
// GetChargePendingRenewals.
func (o *OrdersDB) MarkRenewalChargePending(ctx context.Context, orderID uint) error {
	tag, err := o.pool.Exec(ctx, `UPDATE orders SET "Flag"=$1, updated_at=$2 WHERE id = $3`,
		common.OrderFlagChargePending, time.Now(), orderID)
	if err != nil {
		return fmt.Errorf("o.pool.Exec: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return common.ErrNoRowsAffected
	}
	return nil
}

// PendingCharge is a charge_pending order and the payment whose charge has no
// known outcome: the newest pending payment of the order.
type PendingCharge struct {
	OrderID   uint
	PaymentID int
}

// GetChargePendingRenewals lists the charge_pending orders whose pending payment
// was created before createdBefore, oldest first. The cutoff gives
// external_payments' own reconciliation time to settle the charge first.
func (o *OrdersDB) GetChargePendingRenewals(ctx context.Context, createdBefore time.Time) ([]PendingCharge, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT order_id, payment_id FROM (
			SELECT DISTINCT ON (o.id) o.id AS order_id, p.id AS payment_id, p.created_at
			FROM orders o
			JOIN payments p ON p."OrderID" = o.id
			WHERE o."Flag" = $1 AND p."PaymentStatus" = $2 AND p.deleted_at IS NULL
			ORDER BY o.id, p.id DESC
		) newest
		WHERE created_at < $3
		ORDER BY payment_id`,
		common.OrderFlagChargePending, common.PaymentStatusPending, createdBefore)
	if err != nil {
		return nil, fmt.Errorf("o.pool.Query: %w", err)
	}
	defer rows.Close()

	pending := []PendingCharge{}
	for rows.Next() {
		var pc PendingCharge
		if err := rows.Scan(&pc.OrderID, &pc.PaymentID); err != nil {
			return nil, fmt.Errorf("rows.Scan: %w", err)
		}
		pending = append(pending, pc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows.Err: %w", err)
	}
	return pending, nil
}
