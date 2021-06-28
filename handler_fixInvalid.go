package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleFixInvalid(c *gin.Context) {
	req := `
select * from orders where id in
(select "OrderID" from payments where "PaymentStatus" = 'invalid')
and "Status" = 'paid'
`
	var ordersToCheck []Order
	var totalmarked int
	var totalignored int
	DB.Raw(req).Scan(&ordersToCheck)
	//	fmt.Println(ordersToCheck)

	for i := 0; i < len(ordersToCheck); i++ {
		//		fmt.Println(ordersToCheck[i].ID)
		if thereisanewerorder(ordersToCheck[i]) {
			fmt.Printf("nothing to do for %d\n", ordersToCheck[i].ID)
			totalignored = totalignored + 1
		} else {
			fmt.Printf("mark %d to renew\n", ordersToCheck[i].ID)
			flagOrderForRenewal(uint(ordersToCheck[i].ID))
			totalmarked = totalmarked + 1
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "processed": totalmarked, "skipped": totalignored})
}

/*
/* Get Orders that need to be renewed
/*(select id from orders in (select "OrderID" from payments where "PaymentStatus" = 'invalid'))
/*
/* Then for each order, check if there a new orders if yes, skip
/* if no, mark to renew
*/

func thereisanewerorder(o Order) bool {
	req := `
select * from orders where "AccountID" = ? and created_at > ?
and  "ProductType" = 'globalmembership' and "Status" = 'paid'
`
	var recentorders []Order
	DB.Raw(req, o.AccountID, o.CreatedAt).Scan(&recentorders)
	fmt.Println(recentorders)
	return (len(recentorders) > 0)

}
