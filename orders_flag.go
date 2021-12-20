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
		count := flagOrdersToRenew(body.Month, body.Year)
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

func flagOrdersToRenew(month int64, year int64) int64 {
	// fmt.Println(month)
	// fmt.Println(year)
	// return 2

	// Select all unique individuals who have
	// an active renewable order
	qOPotentialStr := `
	select userkey, count(userkey) as qt
	from orders where ("Status" = 'paid'
	or "Status" = 'nosuccess')
	and "Type" = 'recurring'
	group by userkey
	order by qt desc
	`
	rows, err := DB.Raw(qOPotentialStr).Rows()

	if err != nil {
		fmt.Println("error 1")
		return -1
	}

	type qOPotential struct {
		Userkey string
		Qt      int64
	}

	var aOPotential qOPotential

	defer rows.Close()

	var counter int64
	counter = 0

	for rows.Next() {
		DB.ScanRows(rows, &aOPotential)

		// fmt.Printf("Key: %s  -- Qt: %d\n",
		// 	aOPotential.Userkey,
		// 	aOPotential.Qt)

		qOSelectStr := `
		select * from orders
		where userkey = ?
		and ("Status"='paid'
		or "Status"='nosuccess')
		and "Type" = 'recurring'
		order by "PaymentDate" desc
		limit 1
		`
		oselected, err := DB.Raw(qOSelectStr, aOPotential.Userkey).Rows()

		if err != nil {
			fmt.Println("error 2")
			fmt.Println(err)
			return -1
		}

		defer oselected.Close()
		var aOSelect Order

		for oselected.Next() {
			DB.ScanRows(oselected, &aOSelect)

			//fmt.Println(aOSelect.PaymentDate)
			//fmt.Println(int(aOSelect.PaymentDate.Month()))

			if int64(aOSelect.PaymentDate.Month()) == month && int64(aOSelect.PaymentDate.Year()) == year {
				fmt.Printf("No need to charge order %d\n", aOSelect.ID)
			} else {
				fmt.Printf("Mark Order %d for renewal\n", aOSelect.ID)
				flagOrderForRenewal(uint(aOSelect.ID))
				counter++
			}
		}
	}
	return counter
}

func flagOrderForRenewal(id uint) {
	req := `
		update orders
		set "Flag" = 'torenew'
		where id = ?`

	res := DB.Exec(req, id)

	if res.Error != nil {
		fmt.Println(res.Error)
	}

}
