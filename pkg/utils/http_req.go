package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
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

// PostJSON posts payload and returns the response.
//
// The context is not decoration: a request whose connection is dropped during
// shutdown has its context cancelled, and without carrying that through, the
// outbound call kept running — so the charge to checkout outlived the request
// that started it and the grace it was given. The timeout is the same argument
// for the case where nobody cancels anything.
func PostJSON(ctx context.Context, method string, url string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("http.NewRequestWithContext: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: outboundTimeout}
	return client.Do(req)
}

// outboundTimeout bounds one outbound call. Longer than any shutdown grace on
// purpose: cancellation is what shortens these during a shutdown, and this is
// only the backstop for a peer that accepts a connection and never answers.
const outboundTimeout = 30 * time.Second
