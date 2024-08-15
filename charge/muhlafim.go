package charge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

const (
	muhlafimActionHiyuvNiklat = "חיוב נקלט"
	muhlafimActionNidha       = "נדחה לא יחויב"
	muhlafimActionBitul       = `ביטול הוראת קבע ע"י הלקוח`
	muhlafimActionLoTakin     = "שונה סטאטוס (מתקין ללא תקין)"
)

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

type MuhlafimFetcher interface {
	FetchMuhlafim(ctx context.Context, start time.Time, end time.Time) ([]*TokenRedirect, error)
}

type PelecardMuhlafimFetcher struct{}

func NewPelecardMuhlafimFetcher() *PelecardMuhlafimFetcher {
	return new(PelecardMuhlafimFetcher)
}

func (f *PelecardMuhlafimFetcher) FetchMuhlafim(ctx context.Context, start time.Time, end time.Time) ([]*TokenRedirect, error) {
	request := MuhlafimRequest{
		User:      common.Config.PelecardUser,
		Password:  common.Config.PelecardPassword,
		Terminal:  common.Config.PelecardNewTerminal,
		StartDate: start.Format("02/01/2006 15:04"),
		EndDate:   end.Format("02/01/2006 15:04"),
	}

	b, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("json.Marshal [request]: %w", err)
	}

	resp, err := utils.PostJSON(ctx, http.MethodPost, common.Config.PelecardMuhlafimUrl, b)
	if err != nil {
		return nil, fmt.Errorf("utils.PostJSON: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error [%d], %v, %s", resp.StatusCode, resp.Header, errBody)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("io.ReadAll: %w", err)
	}
	err = os.WriteFile("muhlafim.json", body, 0644)
	if err != nil {
		return nil, fmt.Errorf("os.WriteFile: %w", err)
	}

	var payload MuhlafimResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("json.Unmarshal: %w", err)
	}
	utils.LogFor(ctx).Info("muhlafim response", slog.String("StatusCode", payload.StatusCode),
		slog.String("ErrorMessage", payload.ErrorMessage),
		slog.Int("results_size", len(payload.ResultData)))

	return payload.ResultData, nil
}

func LoadMuhlafimFromFile(path string) ([]*TokenRedirect, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("os.ReadFile [%s]: %w", path, err)
	}

	var resp MuhlafimResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("json.Unmarshal: %w", err)
	}

	byAction := make(map[string][]*TokenRedirect)
	byToken := make(map[string][]*TokenRedirect)
	byNewCard := make(map[string][]*TokenRedirect)
	byOldCard := make(map[string][]*TokenRedirect)
	for _, tr := range resp.ResultData {
		v, ok := byAction[tr.ActionDescription]
		if ok {
			byAction[tr.ActionDescription] = append(v, tr)
		} else {
			byAction[tr.ActionDescription] = []*TokenRedirect{tr}
		}
		v, ok = byToken[tr.Token]
		if ok {
			byToken[tr.Token] = append(v, tr)
		} else {
			byToken[tr.Token] = []*TokenRedirect{tr}
		}
		v, ok = byNewCard[tr.NewCardNumber]
		if ok {
			byNewCard[tr.NewCardNumber] = append(v, tr)
		} else {
			byNewCard[tr.NewCardNumber] = []*TokenRedirect{tr}
		}
		v, ok = byOldCard[tr.CardNumber]
		if ok {
			byOldCard[tr.CardNumber] = append(v, tr)
		} else {
			byOldCard[tr.CardNumber] = []*TokenRedirect{tr}
		}
	}

	for k, v := range byAction {
		slog.Info("", slog.Int(k, len(v)))
	}
	for k, v := range byToken {
		slog.Info("", slog.Int(k, len(v)))
	}
	for k, v := range byNewCard {
		slog.Info("", slog.Int(k, len(v)))
	}
	for k, v := range byOldCard {
		slog.Info("", slog.Int(k, len(v)))
	}

	for k, v := range byAction {
		slog.Info("", slog.Int(k, len(v)))
		for _, x := range v {
			slog.Info("", slog.String("action", x.ActionDescription), slog.String("number", x.CardNumber), slog.String("new_number", x.NewCardNumber), slog.String("expiry", x.NewExpirationDate))
		}
	}

	return resp.ResultData, nil
}
