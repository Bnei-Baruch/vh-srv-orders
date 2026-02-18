package pelecard

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
	Name string `json:"name"`
	PMX  string `json:"pmx"`
}

var TokenTerminal = Terminal{Name: "token", PMX: "t"}
var EMVTerminal = Terminal{Name: "emv", PMX: "e"}
