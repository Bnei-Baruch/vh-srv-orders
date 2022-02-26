package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// routed here from Entrypoint
func handleOrdersFlag(c *gin.Context) {
	type req struct {
		Flag  string `json:"flag"`
		Month int64  `json:"month"`
		Year  int64  `json:"year"`
	}

	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	}

	switch body.Flag {
	case "torenew":
		count := flagOrdersToRenew(c, body.Month, body.Year)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	case "duplicates":
		count := flagDuplicateOrders(body.Flag)
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "flag unknown"})
		return
	}
}

// flagging
func flagOrdersToRenew(c *gin.Context, month int64, year int64) int64 {

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
	rows, qOerr := DB.Query(c, qOPotentialStr)

	if qOerr != nil {
		fmt.Println("error queries orders")
		fmt.Println(qOPotentialStr)
		fmt.Println(">> Error is :")
		fmt.Println(qOerr)
		return -1
	}

	type qOPotential struct {
		Userkey string
		Qt      int64
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

		// err := DB.ScanRows(rows, &aOPotential)

		// if err != nil {
		// 	fmt.Println("Error reading row in scan")
		// 	fmt.Println(err)
		// 	return -1
		// }

		qOSelectStr := `
		select 
		id,
		Type,
		ProductType,
		RecuringFreq,
		AccountID,
		Organization,
		Amount,
		Currency,
		Status,
		OrderLanguage,
		PaymentDate,
		SKU,
		Note,
		Flag,
		created_at,
		updated_at,
		deleted_at from orders
		where userkey = $1
		and ("Status"='paid'
		or "Status"='nosuccess')
		and "ProductType" = 'globalmembership'
		order by "PaymentDate" desc
		limit 1
		`
		oselected, qOSelectStrErr := DB.Query(c, qOSelectStr, aOPotential.Userkey)

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
				&aOSelect.ProductType,
				&aOSelect.RecuringFreq,
				&aOSelect.AccountID,
				&aOSelect.Organization,
				&aOSelect.Amount,
				&aOSelect.Currency,
				&aOSelect.Status,
				&aOSelect.OrderLanguage,
				&aOSelect.PaymentDate,
				&aOSelect.SKU,
				&aOSelect.Note,
				&aOSelect.Flag,
				&aOSelect.CreatedAt,
				&aOSelect.UpdatedAt,
				&aOSelect.DeletedAt)
			if oselectedErr != nil {
				fmt.Println("Error reading row in scan")
				fmt.Println(oselectedErr)
				return -1
			}

			//fmt.Println(aOSelect.PaymentDate)
			//fmt.Println(int(aOSelect.PaymentDate.Month()))

			if int64(aOSelect.PaymentDate.Month()) == month && int64(aOSelect.PaymentDate.Year()) == year {
				fmt.Printf("No need to charge order %d\n", aOSelect.ID)
				continue
			}

			if aOSelect.Type == "regular" {
				fmt.Printf("No need to charge regular order %d\n", aOSelect.ID)
				continue
			}

			// if not this month and not regular, go ahead
			fmt.Printf("Mark Order %d for renewal\n", aOSelect.ID)
			flagOrderForRenewal(c, uint(aOSelect.ID))
			counter++

		}
	}
	return counter
}

func flagOrderForRenewal(ctx *gin.Context, id uint) {
	req := `
		update orders
		set "Flag" = 'torenew'
		where id = $1`

	_, err := DB.Exec(ctx, req, id)

	if err != nil {
		fmt.Println(err)
	}

}
