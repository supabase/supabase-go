// Distributed tracing through the SDK's one HTTP seam: an
// OpenTelemetry-instrumented transport is injected with WithHTTPClient, and
// the SDK's context propagation carries the active span to every outgoing
// request. The SDK itself takes no OpenTelemetry dependency - it lives only in
// this non-published example module, outside the go.work workspace, with
// replace directives resolving the SDK modules from the local tree.
module github.com/supabase/supabase-go/examples/tracing-otel

go 1.25.0

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/postgrest => ../../postgrest
	github.com/supabase/supabase-go/supabase => ../../supabase
)

require (
	github.com/supabase/supabase-go/core v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/postgrest v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/supabase v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0
	go.opentelemetry.io/otel v1.46.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.46.0
	go.opentelemetry.io/otel/sdk v1.46.0
)

// Dependencies of the requirements above, none imported by this program
// itself. The per-line "// indirect" markers are go toolchain notation,
// written and maintained by go mod tidy.
require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/supabase/supabase-go/auth v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
