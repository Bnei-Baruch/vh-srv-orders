package repo

import (
	"context"
	"fmt"
)

func (o *OrdersDB) HardDeleteSpecialByEmail(ctx context.Context, email string) (error, int64) {
	hardDeleteSpecialRes, err := o.Exec(ctx, "DELETE FROM specials WHERE email = $1", email)

	if err != nil {
		return err, 0
	}

	rowsAffected := hardDeleteSpecialRes.RowsAffected()

	return nil, rowsAffected
}

func (o *OrdersDB) GetSpecialByEmail(ctx context.Context, email string) (Special, error) {
	var spe Special

	if err := o.QueryRow(ctx,
		`SELECT 
		email,
		category,
		subcategory
	 	from specials where email = $1`, email).Scan(
		&spe.Email,
		&spe.Category,
		&spe.SubCategory,
	); err != nil {
		return spe, err
	}
	return spe, nil

}

func (o *OrdersDB) HasSpecialMembership(ctx context.Context, email string) bool {
	query := `
select count(s.*) as total
from specials as s
where s."email" = $1
`
	type Results struct {
		Total int
	}
	var r Results
	//var count map[string]interface{}
	//DB.Raw(query, email).Scan(&r)
	// DB.Raw(query, email).Scan(&r)

	if err := o.QueryRow(ctx, query, email).Scan(
		&r.Total,
	); err != nil {
		fmt.Println("--error--", err)
	}

	return r.Total > 0
}
