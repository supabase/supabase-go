// Command tracing-otel propagates W3C trace context on Supabase requests by
// injecting an OpenTelemetry-instrumented transport through
// configuration.WithHTTPClient. The SDK carries each call's context to its
// outgoing request, and otelhttp reads the active span from that context to
// write the traceparent header - the SDK itself needs no tracing
// configuration and takes no OpenTelemetry dependency.
//
// It runs against the local Supabase stack started by
// scripts/integration-test.sh, reading SUPABASE_URL and
// SUPABASE_PUBLISHABLE_KEY from the environment, and prints the exported
// spans to stdout.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// instrument maps a row of the seeded public.instruments table.
type instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	projectURL := os.Getenv("SUPABASE_URL")
	publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || publishableKey == "" {
		fmt.Fprintln(os.Stderr, "SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set - run via scripts/integration-test.sh")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Standard OpenTelemetry wiring, none of it Supabase-specific: export
	// spans to stdout, and register the W3C trace-context propagator that
	// otelhttp writes the traceparent header through (OpenTelemetry's default
	// propagator is a no-op).
	exporter, err := stdouttrace.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdouttrace.New:", err)
		os.Exit(1)
	}
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	defer func() { _ = tracerProvider.Shutdown(ctx) }()
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// The Supabase-specific line: instrument the default transport (a nil base
	// means http.DefaultTransport, the stdlib convention) and inject it through
	// the SDK's single HTTP seam.
	client, err := supabase.New(projectURL, publishableKey,
		configuration.WithHTTPClient(&http.Client{Transport: otelhttp.NewTransport(nil)}))
	if err != nil {
		fmt.Fprintln(os.Stderr, "supabase.New:", err)
		os.Exit(1)
	}

	// In an application the active span usually exists already, opened by
	// HTTP-handler middleware for the inbound request being served, and you
	// pass that request's context to the SDK call. This standalone
	// program has no inbound request, so it opens the span itself. Either way
	// the context carries the span to the injected transport, and the outgoing
	// Supabase request is recorded as a child span in the same trace as the
	// work that caused it.
	queryContext, span := tracerProvider.Tracer("examples/tracing-otel").Start(ctx, "list-instruments")
	instruments, _, err := postgrest.Collect(queryContext, client.Database(),
		postgrest.From[instrument]("instruments").Select("id, name"))
	span.End()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Collect:", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "queried %d instruments inside trace %s\n",
		len(instruments), span.SpanContext().TraceID())
}
