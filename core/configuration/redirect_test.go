package configuration_test

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// redirectStart is the URL each redirect test sends its request to, the one
// redirectingTransport answers with a redirect.
const redirectStart = "https://PROJECT_ID.supabase.co/start"

// arrival is a request as redirectingTransport received it.
type arrival struct {
	url    string
	header http.Header
	body   string
}

// redirectingTransport stands in for the network. It answers a request for
// redirectStart with a 307 redirect to location and any other request with a
// 200, recording every request it receives.
type redirectingTransport struct {
	location string
	arrivals []arrival
}

func (tr *redirectingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		content, err := io.ReadAll(request.Body)
		err = errors.Join(err, request.Body.Close())
		if err != nil {
			return nil, err
		}
		body = content
	}
	tr.arrivals = append(tr.arrivals, arrival{url: request.URL.String(), header: request.Header.Clone(), body: string(body)})

	if request.URL.String() == redirectStart {
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {tr.location}}, Body: http.NoBody, Request: request}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: request}, nil
}

// hop is the URL a request was sent to and the Authorization header it
// carried.
type hop struct {
	url           string
	authorization string
}

// TestRedirects pins which redirects the configured HTTP client follows and
// what it sends on each request it makes: the project key, the global headers
// and the request body to every host it is redirected to, whatever its
// scheme, host or port.
func TestRedirects(t *testing.T) {
	const userAuthorization = "Bearer user-token"
	const payload = `{"password":"correct horse battery staple"}`
	followEvery := func(*http.Request, []*http.Request) error { return nil }
	refuseEvery := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	testCases := []struct {
		name          string
		location      string
		checkRedirect func(*http.Request, []*http.Request) error
		wantStatus    int
		wantErr       string
		wantHops      []hop
	}{
		{"to another host", "https://elsewhere.example/next", nil, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"https://elsewhere.example/next", ""}}},
		{"to a subdomain", "https://eu.PROJECT_ID.supabase.co/next", nil, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"https://eu.PROJECT_ID.supabase.co/next", userAuthorization}}},
		{"to another scheme", "http://PROJECT_ID.supabase.co/next", nil, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"http://PROJECT_ID.supabase.co/next", userAuthorization}}},
		{"to another port", "https://PROJECT_ID.supabase.co:8443/next", nil, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"https://PROJECT_ID.supabase.co:8443/next", userAuthorization}}},
		{"to another host under a policy that follows every redirect", "https://elsewhere.example/next", followEvery, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"https://elsewhere.example/next", ""}}},
		{"within the origin", "https://PROJECT_ID.supabase.co/next", nil, http.StatusOK, "", []hop{{redirectStart, userAuthorization}, {"https://PROJECT_ID.supabase.co/next", userAuthorization}}},
		{"within the origin under a policy that refuses every redirect", "https://PROJECT_ID.supabase.co/next", refuseEvery, http.StatusTemporaryRedirect, "", []hop{{redirectStart, userAuthorization}}},
		{"round a loop within the origin", redirectStart, nil, 0, "stopped after 10 redirects", slices.Repeat([]hop{{redirectStart, userAuthorization}}, 10)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			network := &redirectingTransport{location: testCase.location}
			projectConfiguration, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "project-key",
				configuration.WithHTTPClient(&http.Client{Transport: network, CheckRedirect: testCase.checkRedirect}),
				configuration.WithHeader("X-App-Version", "1.0.0"))
			if err != nil {
				t.Fatalf("configuration.New: %v", err)
			}
			request, err := http.NewRequest(http.MethodPost, redirectStart, strings.NewReader(payload))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			request.Header.Set("Authorization", userAuthorization)

			response, err := projectConfiguration.HTTPClient().Do(request)
			switch {
			case testCase.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Errorf("Do error = %v, want one containing %q", err, testCase.wantErr)
				}
			case err != nil:
				t.Fatalf("Do: %v", err)
			default:
				if response.StatusCode != testCase.wantStatus {
					t.Errorf("status = %d, want %d", response.StatusCode, testCase.wantStatus)
				}
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Errorf("closing the response body: %v", closeErr)
				}
			}

			hops := make([]hop, len(network.arrivals))
			for i, received := range network.arrivals {
				hops[i] = hop{url: received.url, authorization: received.header.Get("Authorization")}
				if got := received.header.Get("apikey"); got != "project-key" {
					t.Errorf("%s: apikey = %q, want %q", received.url, got, "project-key")
				}
				if got := received.header.Get("X-App-Version"); got != "1.0.0" {
					t.Errorf("%s: X-App-Version = %q, want %q", received.url, got, "1.0.0")
				}
				if received.body != payload {
					t.Errorf("%s: body = %q, want %q", received.url, received.body, payload)
				}
			}
			if !slices.Equal(hops, testCase.wantHops) {
				t.Errorf("hops = %+v, want %+v", hops, testCase.wantHops)
			}
		})
	}
}
