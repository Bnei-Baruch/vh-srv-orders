package repo

import (
	"context"
	"fmt"
)

func (o *OrdersDB) FlagOrdersToRenew(ctx context.Context, month int64, year int64) int64 {

	// Select all unique individuals who have
	// an active renewable order
	qOPotentialStr := `
	select userkey, count(userkey) as qt
	from orders where ("Status" = 'paid'
	or "Status" = 'nosuccess')
	and "ProductType" = 'globalmembership'
	group by userkey
	order by qt desc
	`
	rows, qOerr := o.Query(ctx, qOPotentialStr)

	if qOerr != nil {
		fmt.Println("error queries orders")
		fmt.Println(qOPotentialStr)
		fmt.Println(">> Error is :")
		fmt.Println(qOerr)
		return -1
	}

	type qOPotential struct {
		Userkey *string
		Qt      *int64
	}

	var aOPotential qOPotential

	defer rows.Close()

	var counter int64 = 0

	for rows.Next() {

		// var aOSelect Order

		aOerr := rows.Scan(
			&aOPotential.Userkey,
			&aOPotential.Qt)
		if aOerr != nil {
			fmt.Println("Error reading row in scan")
			fmt.Println(aOerr)
			return -1
		}

		qOSelectStr := `
		select 
		id,
		"Type",
		"PaymentDate" from orders
		where userkey = $1
		and ("Status"='paid'
		or "Status"='nosuccess')
		and "ProductType" = 'globalmembership'
		order by "PaymentDate" desc
		limit 1
		`
		oselected, qOSelectStrErr := o.Query(ctx, qOSelectStr, *aOPotential.Userkey)

		if qOSelectStrErr != nil {
			fmt.Println("error 2")
			fmt.Println(qOSelectStrErr)
			return -1
		}

		defer oselected.Close()

		for oselected.Next() {
			var aOSelect Order

			oselectedErr := oselected.Scan(
				&aOSelect.ID,
				&aOSelect.Type,
				&aOSelect.PaymentDate)
			if oselectedErr != nil {
				fmt.Println("Error reading row in scan")
				fmt.Println(oselectedErr)
				return -1
			}

			if int64(aOSelect.PaymentDate.Time.Month()) >= month && int64(aOSelect.PaymentDate.Time.Year()) >= year {
				fmt.Printf("No need to charge order %d\n", aOSelect.ID)
				continue
			}

			if aOSelect.Type.String == "regular" {
				fmt.Printf("No need to charge regular order %d\n", aOSelect.ID)
				continue
			}

			// if not this month and not regular, go ahead
			fmt.Printf("Mark Order %d for renewal\n", aOSelect.ID)
			o.flagOrderForRenewal(ctx, uint(aOSelect.ID))
			counter++

		}
	}
	return counter
}

func (o *OrdersDB) flagOrderForRenewal(ctx context.Context, id uint) {
	req := `
		update orders
		set "Flag" = 'torenew'
		where id = $1`

	_, err := o.Exec(ctx, req, id)

	if err != nil {
		fmt.Println(err)
	}

}
