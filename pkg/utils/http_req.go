package utils

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-resty/resty/v2"
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
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return new(http.Client).Do((req))
}

// APIError is the {"error": "..."} body our own services (accounting,
// profile) answer with on failure. Clients register it with resty's
// SetError so RespError can read it.
type APIError struct {
	Error string `json:"error"`
}

// RespError returns nil unless resty counts the response as an error
// (status >= 400). For an error it returns the service's own message from an
// APIError body, or, when the body has none (not JSON, or JSON without
// "error"), the status and raw body.
func RespError(resp *resty.Response) error {
	if !resp.IsError() {
		return nil
	}
	if apiErr, ok := resp.Error().(*APIError); ok && apiErr.Error != "" {
		return errors.New(apiErr.Error)
	}
	return fmt.Errorf("unexpected response: [%s] %s", resp.Status(), resp.String())
}

// RespStatusError is RespError for outside APIs (Priority, Pelecard) whose
// error body isn't our APIError: nil unless the status is >= 400, otherwise
// "<what> [<status>]: <body>".
func RespStatusError(resp *resty.Response, what string) error {
	if !resp.IsError() {
		return nil
	}
	return fmt.Errorf("%s [%d]: %s", what, resp.StatusCode(), resp.String())
}
