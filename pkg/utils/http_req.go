package utils

import (
	"bytes"
	"fmt"
	"net/http"
)

func PostJSON(method string, url string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("http.NewRequest: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return new(http.Client).Do((req))
}
