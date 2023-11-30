package utils

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

type PelecardCardDetail struct {
	StatusCode   string `json:"StatusCode"`
	ErrorMessage string `json:"ErrorMessage"`
	ResultData   struct {
		CreditCardNumber string `json:"CreditCardNumber"`
		ExpirationDate   string `json:"ExpirationDate"`
	} `json:"ResultData"`
}

func FetchPelecardCardDetailFromToken(token string, terminalNumber string) (PelecardCardDetail, error) {
	if len(token) == 0 {
		return PelecardCardDetail{}, fmt.Errorf("empty token passed")
	}

	var postBody []byte

	pelecardFullUrl := "https://gateway20.pelecard.biz/services/ConvertToCC"

	postBody, _ = json.Marshal(map[string]interface{}{
		"terminalNumber": terminalNumber,
		"user":           common.Config.PelecardUser,
		"password":       common.Config.PelecardPassword,
		"token":          token,
	})

	buffPostBody := bytes.NewBuffer(postBody)

	pelecardRes, _ := HTTPCallAndGetBody(pelecardFullUrl, "", buffPostBody, "POST")
	// TODO (edo): handle status code

	var pelecardResponse PelecardCardDetail
	if err := json.Unmarshal(pelecardRes, &pelecardResponse); err != nil {
		return pelecardResponse, err
	}

	return pelecardResponse, nil

}
