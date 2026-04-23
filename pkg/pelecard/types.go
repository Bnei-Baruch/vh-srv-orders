package pelecard

import "context"

const (
	PELECARD_API_BASE_URL = "https://gateway20.pelecard.biz"

	// Action description constants
	MUH_HIYUV_NIKLAT = "חיוב נקלט"
	MUH_NIDHA        = "נדחה לא יחויב"
	MUH_BITUL        = "ביטול הוראת קבע ע\"י הלקוח"
	MUH_LOTAKIN      = "שונה סטאטוס (מתקין ללא תקין)"
)

type BaseRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type TerminalRequest struct {
	BaseRequest
	TerminalNumber string `json:"terminalNumber"`
}

// MuhlafimRequest represents the request payload for the Pelecard muhlafim API
type MuhlafimRequest struct {
	TerminalRequest
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

func NewMuhlafimRequest(terminalRequest TerminalRequest, startDate, endDate string) *MuhlafimRequest {
	return &MuhlafimRequest{
		TerminalRequest: terminalRequest,
		StartDate:       startDate,
		EndDate:         endDate,
	}
}

// MuhlafimResponse represents the response from the Pelecard muhlafim API
type MuhlafimResponse struct {
	ResultData []MuhlafimEntry `json:"ResultData"`
}

// MuhlafimEntry represents a single muhlafim entry from the API
type MuhlafimEntry struct {
	Token             string `json:"Token"`
	ActionDescription string `json:"ActionDescription"`
	NewCardNumber     string `json:"NewCardNumber"`
	NewExpirationDate string `json:"NewExpirationDate"`
}

type Terminal struct {
	Name      string `json:"name"`
	PMX       string `json:"pmx"`
	ChargeURL string `json:"-"`
}

var TokenTerminal = Terminal{Name: "token", PMX: "t", ChargeURL: "https://checkout.kbb1.com/token/charge"}
var EMVTerminal = Terminal{Name: "emv", PMX: "e", ChargeURL: "https://checkout.kbb1.com/emv/charge"}

// TerminalByPMX returns the Terminal for a given PMX value.
// PMX is the canonical identifier used across internal code, Priority, and Pelecard.
func TerminalByPMX(pmx string) Terminal {
	switch pmx {
	case TokenTerminal.PMX:
		return TokenTerminal
	case EMVTerminal.PMX:
		return EMVTerminal
	default:
		return Terminal{PMX: pmx}
	}
}

// ChargeRequest is the payment gateway request for token-based charges.
type ChargeRequest struct {
	// Pelecard gateway fields
	GoodURL    string `json:"GoodURL"`
	ErrorURL   string `json:"ErrorURL"`
	CancelURL  string `json:"CancelURL"`
	ApprovalNo string `json:"ApprovalNo"`
	Token      string `json:"Token"`

	// Payment / Priority fields
	Name         string  `json:"Name"`
	Price        float64 `json:"Price"`
	Currency     string  `json:"Currency"`
	Email        string  `json:"Email"`
	Phone        string  `json:"Phone"`
	Street       string  `json:"Street"`
	City         string  `json:"City"`
	Country      string  `json:"Country"`
	Participans  string  `json:"Participants"`
	Details      string  `json:"Details"`
	SKU          string  `json:"SKU"`
	VAT          string  `json:"VAT"`
	Installments int     `json:"Installments"`
	Language     string  `json:"Language"`
	Reference    string  `json:"Reference"`
	Organization string  `json:"Organization"`
	UserKey      string  `json:"UserKey"`
}

// ChargeExecutor executes a payment charge. Implementations include the live gateway
// client and the dry-run executor for testing.
type ChargeExecutor interface {
	Execute(ctx context.Context, request *ChargeRequest, terminal Terminal, orderID uint) (response map[string]interface{}, err error)
}
