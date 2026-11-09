package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
)

// transportError is a request that got no answer: the transport could not
// carry it, or its response could not be read. It wraps the failure as the
// caller sees it, so it renders and unwraps as that failure does.
type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }

// Unwrap returns the failure as the caller sees it.
func (e *transportError) Unwrap() error { return e.err }

// apiVersion is the Auth API version every request states. It selects the
// shape of the error bodies the server answers with.
const apiVersion = "2024-01-01"

// send executes one request through exchange for the calls that read no
// response headers.
func send(ctx context.Context, httpClient configuration.HTTPClient, method string, endpoint *url.URL, payload any, bearer string) ([]byte, error) {
	body, _, err := exchange(ctx, httpClient, method, endpoint, payload, bearer)
	return body, err
}

// exchange executes one request to the Auth service and returns the response
// body and headers. The request states apiVersion in the
// X-Supabase-Api-Version header. A payload is JSON-encoded, a nil payload
// sends no body, a non-empty bearer carries an end user's token on the
// Authorization header in place of the configured key, and a non-2xx answer
// is shaped into [*Error]. A request that gets no answer fails with a
// [*transportError].
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
		return nil, nil, &transportError{fmt.Errorf("auth: executing request: %w", err)}
	}
	responseBody, err := readAll(response.Body)
	if err != nil {
		return nil, nil, &transportError{fmt.Errorf("auth: reading response: %w", err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, newError(response.StatusCode, responseBody, parseRetryAfter(response.Header))
	}
	return responseBody, response.Header, nil
}

// readAll reads body to its end and closes it, even when a read panics. It
// fails when the read or the close fails.
func readAll(body io.ReadCloser) (content []byte, err error) {
	defer func() { err = errors.Join(err, body.Close()) }()
	return io.ReadAll(body)
}

// parseRetryAfter returns the wait a Retry-After header asks for, stated in
// whole seconds, and zero when the header is absent or in any other form.
func parseRetryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
