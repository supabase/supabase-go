// Package respond writes the responses of the test servers in the postgrest
// module's tests. A failed write fails the test without stopping it, so a
// handler may call these functions from the server's goroutine.
package respond

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Body writes body as the response body.
func Body(t *testing.T, writer http.ResponseWriter, body []byte) {
	t.Helper()
	if _, err := writer.Write(body); err != nil {
		t.Errorf("writing the response body: %v", err)
	}
}

// JSON writes value, encoded as JSON, as the response body.
func JSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encoding the response body: %v", err)
	}
}
