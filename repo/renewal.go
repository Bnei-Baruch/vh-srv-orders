package repo

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
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

	prevPayment, err := o.GetPaymentForOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: o.GetPaymentForOrderID: %w", common.ErrPrePayment, err)
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
	if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO payments (%s) VALUES (%s) RETURNING id`, createString, numString),
		createQueryArgs...).Scan(&p.ID); err != nil {
		return nil, fmt.Errorf("o.QueryRow.Scan [insert payment]: %w", err)
	}

	createPelecardString, numPelecardString, createPelecardQueryArgs := preparePelecardPaymentCreateQuery(p, p.ID)
	_, err := o.Exec(ctx, fmt.Sprintf(`INSERT INTO payments_pelecard (%s) VALUES (%s)`, createPelecardString, numPelecardString),
		createPelecardQueryArgs...)
	if err != nil {
		return nil, fmt.Errorf("o.Exec [insert pelecard]: %w", err)
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

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(p)
	_, err = o.Exec(ctx, fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, p.ID), toUpdateArgs...)
	if err != nil {
		return nil, fmt.Errorf("o.Exec [update payment]: %w", err)
	}

	return &p, nil
}

// FinalizeRenewal persists the gateway result in a single transaction.
// Updates payment status, order flag/status, and pelecard_payment record.
// Does NOT emit events — the billing domain is responsible for that.
func (o *OrdersDB) FinalizeRenewal(ctx context.Context, orderID uint, payment *Payment) error {
	tx, err := o.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: o.Begin: %w", common.ErrPostPayment, err)
	}
	defer tx.Rollback(ctx)

	if payment.Success.String == "1" {
		if _, err := tx.Exec(ctx, `UPDATE orders SET "Flag"=$1, updated_at=$2 WHERE id = $3`,
			common.OrderFlagRenewed, time.Now(), orderID); err != nil {
			return fmt.Errorf("%w: tx.Exec [flag renewed]: %w", common.ErrPostPayment, err)
		}
	}

	toUpdate, toUpdateArgs := preparePaymentUpdateQuery(*payment)
	_, err = tx.Exec(ctx,
		fmt.Sprintf(`UPDATE payments SET %s WHERE id=%d`, toUpdate, payment.ID), toUpdateArgs...)
	if err != nil {
		return fmt.Errorf("%w: tx.Exec [update payment]: %w", common.ErrPostPayment, err)
	}

	toUpdatePelecard, toUpdateArgsPeleCard := preparePelecardPaymentUpdateViaPaymentStructQuery(*payment)
	_, err = tx.Exec(ctx,
		fmt.Sprintf(`UPDATE payments_pelecard SET %s WHERE payment_id=%d`, toUpdatePelecard, payment.ID),
		toUpdateArgsPeleCard...)
	if err != nil {
		return fmt.Errorf("%w: tx.Exec [update pelecard_payment]: %w", common.ErrPostPayment, err)
	}

	now := time.Now()
	if payment.Success.String == "1" {
		_, err = tx.Exec(ctx, `UPDATE orders SET "Status"=$1, "PaymentDate"=$2, updated_at=$3 WHERE id = $4`,
			common.OrderStatusPaid, now, now, payment.OrderID.Int)
	} else {
		_, err = tx.Exec(ctx, `UPDATE orders SET "Status"=$1, updated_at=$2 WHERE id = $3`,
			common.OrderStatusNoSuccess, now, payment.OrderID.Int)
	}
	if err != nil {
		return fmt.Errorf("%w: tx.Exec [update order status]: %w", common.ErrPostPayment, err)
	}

	return tx.Commit(ctx)
}
