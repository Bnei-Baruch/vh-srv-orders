package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type PeleCardRecord struct {
	ParamX        string
	AuthNo        string
	TransactionID string

	CCBrand   string
	CCExpDate string
	CCNumber  string

	Pelecard_token string

	DebitCurrency string
	DebitTotal    string
}

func handleFixPelecard(c *gin.Context) {
	fmt.Println("handleFixPelecard")

	type req struct {
		Data []PeleCardRecord `json:"data"`
	}

	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	}

	//convert mbx into payment ID

	var count int
	for i := 0; i < len(body.Data); i++ {
		if updatePaymentwithRecord(body.Data[i]) {
			count++
		}
	}

	//count = 1
	updatePaymentwithRecord(body.Data[922])

	c.JSON(http.StatusOK, gin.H{"Success": count})
}

func getPaymentIDFromMbX(mbx string) string {
	mbs := strings.Split(mbx, "-")
	return mbs[1]
}

func updatePaymentwithRecord(pc PeleCardRecord) bool {
	// prepare the data
	//
	pid := getPaymentIDFromMbX(pc.ParamX)
	var currency string
	switch pc.DebitCurrency {
	case "$":
		currency = "2"
	case "₪":
		currency = "1"
	case "€":
		currency = "0"
	}

	debitTotal := pc.DebitTotal + "00"

	req := `
select * from payments
where id = ?
`
	type result struct {
		ID            int64     `gorm:"column:id"`
		PaymentStatus string    `gorm:"column:PaymentStatus"`
		Amount        string    `gorm:"column:Amount"`
		OrderID       string    `gorm:"column:OrderID"`
		CreatedAt     time.Time `gorm:"column:created_at"`
	}
	var res result
	DB.Raw(req, pid).Scan(&res)

	if res.PaymentStatus == "pending" {
		fmt.Println(res)
	}

	ureq := `
update payments
set "PaymentStatus"='success',
"success"='1',
"ParamX"=?,
"AuthNo"=?,
"TransactionID"=?,
"CCBrand"=?,
"CCExpDate"=?,
"CCNumber"=?,
pelecard_token=?,
"DebitCurrency"=?,
"DebitTotal"=?
where id=?
and "PaymentStatus" = 'pending'
`

	err := DB.Exec(ureq, pc.ParamX, pc.AuthNo, pc.TransactionID,
		pc.CCBrand, pc.CCExpDate, pc.CCNumber, pc.Pelecard_token, currency, debitTotal, pid).Error

	if err != nil {
		fmt.Println(err)
		return false
	}

	ordreq := `
update orders
set "Status"='paid',
"PaymentDate"=?
where id = ?
`
	err = DB.Exec(ordreq, res.CreatedAt, res.OrderID).Error

	if err != nil {
		fmt.Println(err)
	}

	return true

}
