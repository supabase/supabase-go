// Package telemetry provides utilities to provide informative and semantically correct
// data to Supabase services that helps identify this SDK.
package telemetry

// ClientInformationHeaderValue returns a value suitable for use for the
// X-Client-Info header sent with HTTP requests submitted to Supabase services.
func ClientInformationHeaderValue() string {
	return "supabase-go"
}
