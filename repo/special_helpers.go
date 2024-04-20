package repo

import (
	"context"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

func (o *OrdersDB) HardDeleteSpecialByEmail(ctx context.Context, email string) error {
	res, err := o.Exec(ctx, "DELETE FROM specials WHERE email = $1", email)
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

	if err := o.QueryRow(ctx, `SELECT email, category, subcategory from specials where email = $1`, email).
		Scan(&spe.Email, &spe.Category, &spe.SubCategory); err != nil {
		return nil, err
	}

	return &spe, nil
}

func (o *OrdersDB) HasSpecialMembership(ctx context.Context, email string) (bool, error) {
	count, err := o.count(ctx, `select count(s.*) as total from specials as s where s."email" = $1`, email)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
