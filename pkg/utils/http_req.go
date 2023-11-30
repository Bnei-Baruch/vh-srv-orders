package utils

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func HTTPCallAndGetBody(fullUrl string, authHeader string, bodyBuffer *bytes.Buffer, typeOfReq string) ([]byte, int) {

	// Send req using http Client
	client := &http.Client{}

	var req *http.Request
	var err error

	if bodyBuffer != nil {
		req, err = http.NewRequest(typeOfReq, fullUrl, bodyBuffer)
	} else {
		req, err = http.NewRequest(typeOfReq, fullUrl, nil)
	}
	if err != nil {
		fmt.Println("Error while creating new request ::", err)
		return nil, 0
	}

	if authHeader != "" {
		req.Header.Add("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error while creating the data ::", err)
		return nil, 0
	}

	// To avoid memory leak if the connection is left open
	defer resp.Body.Close()

	// Read all the data until EOF as byte
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error while parsing the body ::", err)
		return nil, 0
	}

	return body, resp.StatusCode
}

func PostJSON(method string, url string, payload []byte) (*http.Response, error) {
	fmt.Println("POSTING TO ENDPOINT")
	payReq, _ := http.NewRequest(method, url, bytes.NewReader(payload))
	payReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(payReq)
	if err != nil {
		return nil, err
	}
	return resp, err

}
