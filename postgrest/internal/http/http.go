// Package http executes PostgREST requests: [Client.Do] runs the exchange
// loop for one built request, assembling headers, retrying transient
// failures within the automatic-retry contract and renewing a rejected
// access token through the attached resolver, then reports the final
// response's status and body verbatim as a [Result]. A completed exchange is
// never an error here, whatever its status code, so the caller shapes
// user-facing failures from the Result it receives.
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// TokenResolver returns the end-user access token an exchange sends as the
// credentials of the Bearer authentication scheme on the Authorization
// header. [Client.Do] calls it before the first attempt and again after the
// server rejects a sent token with HTTP 401, propagating its error verbatim
// as the exchange's failure. A resolver must return a non-empty token or an
// error.
type TokenResolver func(ctx context.Context) (string, error)

// Client carries the context every exchange shares - the transport, base
// URL, automatic-retry default, schema, Accept value, representation
// preference and token resolver - and sends one built request per
// [Client.Do] call. A Client is an immutable value: the With methods return
// altered copies that share the underlying transport, so a copy derived for
// one call leaves its source untouched. Construct with [New].
type Client struct {
	transport      configuration.HTTPClient
	baseURL        url.URL
	retry          bool
	schema         string
	accept         string
	representation bool
	resolveToken   TokenResolver
}

// New constructs a [Client] sending every request through transport to a
// path under baseURL, with automatic retries on by default when retry is
// true and an Accept header of application/json until [Client.WithAccept]
// replaces it.
func New(transport configuration.HTTPClient, baseURL url.URL, retry bool) Client {
	return Client{
		transport: transport,
		baseURL:   baseURL,
		retry:     retry,
		accept:    "application/json",
	}
}

// WithTokenResolver returns a copy of this Client that resolves an end-user
// access token through resolve for every exchange. A nil resolve sends no
// Authorization header.
func (c Client) WithTokenResolver(resolve TokenResolver) Client {
	c.resolveToken = resolve
	return c
}

// WithSchema returns a copy of this Client that names schema on the profile
// header PostgREST reads for each request's method: Accept-Profile on GET
// and HEAD, Content-Profile otherwise. An empty schema sends no profile
// header.
func (c Client) WithSchema(schema string) Client {
	c.schema = schema
	return c
}

// WithRetry returns a copy of this Client with automatic retries forced on
// or off, replacing the construction-time default.
func (c Client) WithRetry(enabled bool) Client {
	c.retry = enabled
	return c
}

// WithAccept returns a copy of this Client sending value as the Accept
// header of every request.
func (c Client) WithAccept(value string) Client {
	c.accept = value
	return c
}

// WithRepresentation returns a copy of this Client that asks a write to
// return the rows it affects, adding Prefer: return=representation when the
// request's method is not GET or HEAD.
func (c Client) WithRepresentation() Client {
	c.representation = true
	return c
}

// Result is a completed exchange: the final response, read in full, whatever
// its status code. Do returns it to the caller, which owns it outright.
type Result struct {
	// StatusCode is the HTTP status code of the final response.
	StatusCode int

	// Body is the response body, read in full.
	Body []byte

	// ContentRange is the final response's Content-Range header value, empty
	// when the server sent none.
	ContentRange string
}

// Do sends the request and returns the completed exchange's [Result].
// Failures - a request that cannot be built, an unreachable server after any
// retries, a failed token resolution - return as errors, while any completed
// exchange is a Result, its status code untouched.
func (c Client) Do(ctx context.Context, r request.Request) (Result, error) {
	var accessToken string
	if c.resolveToken != nil {
		token, err := c.resolveToken(ctx)
		if err != nil {
			return Result{}, err
		}
		accessToken = token
	}

	retryable := c.retry && retryableMethods[r.Method()]

	for attempt := 0; ; attempt++ {
		httpRequest, err := r.HTTPRequest(ctx, &c.baseURL)
		if err != nil {
			return Result{}, fmt.Errorf("postgrest: building request: %w", err)
		}

		if accessToken != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
		}
		httpRequest.Header.Set("Accept", c.accept)

		// The schema-selection profile header: PostgREST reads Accept-Profile on
		// GET and HEAD and Content-Profile on every other method.
		if c.schema != "" {
			if httpRequest.Method == http.MethodGet || httpRequest.Method == http.MethodHead {
				httpRequest.Header.Set("Accept-Profile", c.schema)
			} else {
				httpRequest.Header.Set("Content-Profile", c.schema)
			}
		}

		// The Prefer header is composed here, the single place it is set: the
		// builder's own preference (an upsert's resolution) followed by the
		// representation this exchange asks for. Appending keeps both as
		// separate field-lines the server reads as one comma-separated list per
		// RFC 7240.
		if preference := r.Preference(); preference != "" {
			httpRequest.Header.Add("Prefer", preference)
		}
		if c.representation &&
			httpRequest.Method != http.MethodGet && httpRequest.Method != http.MethodHead {
			httpRequest.Header.Add("Prefer", "return=representation")
		}

		if attempt > 0 {
			httpRequest.Header.Set("X-Retry-Count", strconv.Itoa(attempt))
		}

		httpResponse, err := c.transport.Do(httpRequest)
		if err != nil {
			if retryable && attempt < MaximumRetries && ctx.Err() == nil &&
				RetrySleep(ctx, retryDelay(attempt, "")) == nil {
				continue
			}
			return Result{}, fmt.Errorf("postgrest: executing request: %w", err)
		}

		if retryable && attempt < MaximumRetries && retryableStatusCodes[httpResponse.StatusCode] {
			delay := retryDelay(attempt, httpResponse.Header.Get("Retry-After"))
			_, _ = io.Copy(io.Discard, httpResponse.Body)
			_ = httpResponse.Body.Close()
			if RetrySleep(ctx, delay) == nil {
				continue
			}
			return Result{}, fmt.Errorf("postgrest: executing request: %w", ctx.Err())
		}

		responseBody, err := io.ReadAll(httpResponse.Body)
		_ = httpResponse.Body.Close()
		if err != nil {
			return Result{}, fmt.Errorf("postgrest: reading response: %w", err)
		}

		// A server rejection of the sent token (HTTP 401) re-asks the resolver
		// and re-sends immediately with whatever it returns: no backoff, no
		// token comparison, any HTTP method, spending from the same per-call
		// budget as a transient retry. The resolver gate is true only when a
		// resolver-supplied token went out on this attempt, so an API-key-only
		// exchange's 401 surfaces untouched below.
		if httpResponse.StatusCode == http.StatusUnauthorized &&
			c.resolveToken != nil && attempt < MaximumRetries {
			renewed, err := c.resolveToken(ctx)
			if err != nil {
				return Result{}, err
			}
			accessToken = renewed
			continue
		}

		return Result{
			StatusCode:   httpResponse.StatusCode,
			Body:         responseBody,
			ContentRange: httpResponse.Header.Get("Content-Range"),
		}, nil
	}
}

// MaximumRetries is how many times one exchange is re-sent after its first
// attempt fails in a retryable way.
const MaximumRetries = 3

// retryableMethods holds the HTTP methods whose requests are safe to repeat
// and so may be retried automatically.
var retryableMethods = map[string]bool{
	http.MethodGet:  true,
	http.MethodHead: true,
}

// retryableStatusCodes holds the response statuses treated as transient:
// 503, sent while the service cannot reach or is rebuilding its view of the
// database, and 520, sent by fronting infrastructure for a transient origin
// failure.
var retryableStatusCodes = map[int]bool{
	http.StatusServiceUnavailable: true,
	520:                           true,
}

// retryDelay returns the wait before the retry that follows the zero-based
// attempt: one second doubled per attempt, or the whole seconds requested by
// a parseable non-negative retryAfterHeader.
func retryDelay(attempt int, retryAfterHeader string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfterHeader)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second << attempt
}

// RetrySleep pauses for the given duration, returning early with ctx's error
// when ctx ends first and nil after a full pause. It is a variable so tests
// substitute an instantaneous recorder.
var RetrySleep = func(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
