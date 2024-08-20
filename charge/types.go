package charge

import "gitlab.bbdev.team/vh/pay/orders/repo"

type MonthlyChargeContext struct {
	repo.MonthlyCharge
	ordersToCancel map[int]*OrderToCancel
}

type OrderToCancel struct {
	OrderID   int
	Redirect  *TokenRedirect
	CardID    int
	PaymentID int
}

type MuhlafimRequest struct {
	User      string
	Password  string
	Terminal  string `json:"terminalNumber"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type TokenRedirect struct {
	SupplierNumber    string `json:"SupplierNumber"`
	CreationDate      string `json:"CreationDate"` // "20/06/2024 14:09:12"
	Source            string `json:"Source"`
	FileName          string `json:"FileName"`
	ActionDescription string `json:"ActionDescription"`
	Token             string `json:"Token"`
	CardNumber        string `json:"CardNumber"`
	NewCardNumber     string `json:"NewCardNumber"`
	NewExpirationDate string `json:"NewExpirationDate"`
	Amount            string `json:"Amount"`
	VoucherNo         string `json:"VoucherNo"`
}

type MuhlafimResponse struct {
	StatusCode   string           `json:"StatusCode"`
	ErrorMessage string           `json:"ErrorMessage"`
	ResultData   []*TokenRedirect `json:"ResultData"`
}
