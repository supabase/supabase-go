package configuration_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// capturingHandler retains every record it handles, so tests can assert on
// emission count, level, message and attributes.
type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingHandler) WithGroup(string) slog.Handler { return h }

// attributesOf flattens a record into key-to-rendered-value form.
func attributesOf(record slog.Record) map[string]string {
	attributes := make(map[string]string, record.NumAttrs())
	record.Attrs(func(attribute slog.Attr) bool {
		attributes[attribute.Key] = attribute.Value.String()
		return true
	})
	return attributes
}

func TestWithLoggerEmitsOneDebugLogPerRequest(t *testing.T) {
	server, _ := recordingServer(t)
	handler := &capturingHandler{}
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "API_KEY",
		configuration.WithLogger(slog.New(handler)))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}

	requestURL := server.URL + "/rest/v1/instruments?select=name,secret_column"
	sendGet(t, projectConfiguration.HTTPClient(), requestURL, nil)
	sendGet(t, projectConfiguration.HTTPClient(), requestURL, nil)

	if len(handler.records) != 2 {
		t.Fatalf("emissions = %d, want exactly one per request", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelDebug {
		t.Errorf("level = %v, want %v", record.Level, slog.LevelDebug)
	}
	if record.Message != "supabase: request completed" {
		t.Errorf("message = %q, want %q", record.Message, "supabase: request completed")
	}

	attributes := attributesOf(record)
	for _, key := range []string{"method", "host", "path", "status", "duration"} {
		if _, present := attributes[key]; !present {
			t.Errorf("attribute %q missing from the emission", key)
		}
	}
	if len(attributes) != 5 {
		t.Errorf("attribute count = %d (%v), want 5", len(attributes), attributes)
	}
	if attributes["path"] != "/rest/v1/instruments" {
		t.Errorf("path = %q, want %q", attributes["path"], "/rest/v1/instruments")
	}
	if attributes["status"] != "200" {
		t.Errorf("status = %q, want %q", attributes["status"], "200")
	}
	for key, value := range attributes {
		if strings.Contains(value, "API_KEY") || strings.Contains(value, "secret_column") {
			t.Errorf("attribute %s = %q leaks a credential or query string", key, value)
		}
	}
}

// errBrokenWire stands in for a network-level transport failure.
var errBrokenWire = errors.New("broken wire")

// failingTransport fails every round trip with errBrokenWire.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errBrokenWire
}

func TestWithLoggerLogsTransportFailure(t *testing.T) {
	handler := &capturingHandler{}
	projectConfiguration, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "API_KEY",
		configuration.WithHTTPClient(&http.Client{Transport: failingTransport{}}),
		configuration.WithLogger(slog.New(handler)))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, "https://PROJECT_ID.supabase.co/rest/v1/instruments", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := projectConfiguration.HTTPClient().Do(request); !errors.Is(err, errBrokenWire) {
		t.Fatalf("Do error = %v, want the transport failure to reach the caller", err)
	}

	if len(handler.records) != 1 {
		t.Fatalf("emissions = %d, want 1", len(handler.records))
	}
	record := handler.records[0]
	if record.Message != "supabase: request failed" {
		t.Errorf("message = %q, want %q", record.Message, "supabase: request failed")
	}
	attributes := attributesOf(record)
	if !strings.Contains(attributes["error"], errBrokenWire.Error()) {
		t.Errorf("error attribute = %q, want it to carry %q", attributes["error"], errBrokenWire)
	}
	if _, present := attributes["status"]; present {
		t.Errorf("status attribute = %q, want it absent when no response arrived", attributes["status"])
	}
}

// disabledHandler reports every level disabled and fails the test if an
// emission reaches Handle anyway, pinning the transport's Enabled guard: a
// logger that filters out debug costs no attribute construction.
type disabledHandler struct {
	t *testing.T
}

func (h disabledHandler) Enabled(context.Context, slog.Level) bool { return false }

func (h disabledHandler) Handle(context.Context, slog.Record) error {
	h.t.Error("Handle invoked although Enabled reported false")
	return nil
}

func (h disabledHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h disabledHandler) WithGroup(string) slog.Handler { return h }

func TestLoggingSkippedWhenLevelDisabled(t *testing.T) {
	server, _ := recordingServer(t)
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "API_KEY",
		configuration.WithLogger(slog.New(disabledHandler{t: t})))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, nil)
}

func TestWithLoggerNilIgnored(t *testing.T) {
	server, _ := recordingServer(t)
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "API_KEY",
		configuration.WithLogger(nil))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	// The discard default stays in place, so the request must succeed silently.
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, nil)
}
