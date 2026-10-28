package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/supabase/supabase-go/core/configuration"
)

// apiVersion is the Auth API version every request states. It selects the
// shape of the error bodies the server answers with.
const apiVersion = "2024-01-01"

// send executes one request through exchange for the calls that read no
// response headers and present no user token.
func send(ctx context.Context, httpClient configuration.HTTPClient, method string, endpoint *url.URL, payload any) ([]byte, error) {
	body, _, err := exchange(ctx, httpClient, method, endpoint, payload, "")
	return body, err
}

// exchange executes one request to the Auth service and returns the response
// body and headers. The request states apiVersion in the
// X-Supabase-Api-Version header. A payload is JSON-encoded, a nil payload
// sends no body, a non-empty bearer carries an end user's token on the
// Authorization header in place of the configured key, and a non-2xx answer
// is shaped into [*Error].
func exchange(ctx context.Context, httpClient configuration.HTTPClient, method string, endpoint *url.URL, payload any, bearer string) ([]byte, http.Header, error) {
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: encoding request body: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: building request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	request.Header.Set("X-Supabase-Api-Version", apiVersion)

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: executing request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: reading response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, newError(response.StatusCode, responseBody)
	}
	return responseBody, response.Header, nil
}
