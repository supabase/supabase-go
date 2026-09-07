package postgrest

import (
	"net/http"
	"testing"
)

// The tests in this file substitute the package-level retrySleep seam through
// stubRetrySleep, so they must not call t.Parallel.

// TestRPCPostModesAreNeverRetried proves every POST-verb function call is sent
// exactly once even with retries forced on, because only GET and HEAD are
// retryable: the rows, value and void shapes all ride a POST.
func TestRPCPostModesAreNeverRetried(t *testing.T) {
	testCases := []struct {
		name   string
		invoke func(t *testing.T, client *Client) error
	}{
		{
			name: "Rows through Collect",
			invoke: func(t *testing.T, client *Client) error {
				_, _, err := Collect(t.Context(), client, RPC[map[string]any]("f").Rows(), WithRetry(true))
				return err
			},
		},
		{
			name: "Value through CollectRaw",
			invoke: func(t *testing.T, client *Client) error {
				_, _, err := CollectRaw(t.Context(), client, RPC[int]("f").Value(), WithRetry(true))
				return err
			},
		},
		{
			name: "Void through Execute",
			invoke: func(t *testing.T, client *Client) error {
				_, err := Execute(t.Context(), client, RPCVoid("f"), WithRetry(true))
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stubRetrySleep(t)
			server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
			client := newRetryTestClient(t, server.URL)

			if err := testCase.invoke(t, client); err == nil {
				t.Fatal("call succeeded, want an error")
			}
			if got := len(*retryCounts); got != 1 {
				t.Errorf("requests = %d, want 1 (a POST call is never retried, even with WithRetry(true))", got)
			}
		})
	}
}

// TestRPCReadOnlyModesAreRetried proves the read-only GET forms inherit the
// automatic-retry contract untouched: a call answered with a transient 503 is
// re-sent the full complement of times, because a GET is retryable.
func TestRPCReadOnlyModesAreRetried(t *testing.T) {
	testCases := []struct {
		name   string
		invoke func(t *testing.T, client *Client) error
	}{
		{
			name: "Rows ReadOnly through Collect",
			invoke: func(t *testing.T, client *Client) error {
				_, _, err := Collect(t.Context(), client, RPC[map[string]any]("f").Rows().ReadOnly(), WithRetry(true))
				return err
			},
		},
		{
			name: "Value ReadOnly through CollectRaw",
			invoke: func(t *testing.T, client *Client) error {
				_, _, err := CollectRaw(t.Context(), client, RPC[int]("f").Value().ReadOnly(), WithRetry(true))
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stubRetrySleep(t)
			server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
			client := newRetryTestClient(t, server.URL)

			if err := testCase.invoke(t, client); err == nil {
				t.Fatal("call succeeded, want an error")
			}
			if got := len(*retryCounts); got != 1+maximumRetries {
				t.Errorf("requests = %d, want %d (a read-only GET call retries)", got, 1+maximumRetries)
			}
		})
	}
}
