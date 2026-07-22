// Command telemetrytest asserts the X-Client-Info header each SDK entry point
// sends when the SDK is consumed from outside its own repository. The
// TELEMETRY_TEST_MODE environment variable selects the expectation:
// "replaced-dependencies" asserts the fabricated versions go.mod requires,
// proving they were resolved from the running binary's build-information
// dependency records, while "no-module-information" asserts the 0.0.0
// fallback of a binary that carries no module records, as GOPATH-mode builds
// produce. The program exits non-zero when any assertion fails.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"

	"github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/postgrest"
)

// requiredRootVersion and requiredPostgrestVersion mirror the require
// directives in go.mod, stripped of their "v" prefix.
const (
	requiredRootVersion      = "0.999.1-fabricated"
	requiredPostgrestVersion = "0.999.2-fabricated"
)

func main() {
	expectedRootVersion, expectedPostgrestVersion := expectedVersions()

	runtimeSuffix := "; runtime=go"
	if goRuntimeVersion, didFindGoPrefix := strings.CutPrefix(runtime.Version(), "go"); didFindGoPrefix {
		runtimeSuffix += "; runtime-version=" + goRuntimeVersion
	}

	checks := []struct {
		name     string
		want     string
		exercise func(baseURL string) error
	}{
		{
			name:     "root supabase client",
			want:     "supabase-go/" + expectedRootVersion + runtimeSuffix,
			exercise: exerciseRootClient,
		},
		{
			name:     "standalone postgrest client",
			want:     "postgrest-go/" + expectedPostgrestVersion + runtimeSuffix,
			exercise: exercisePostgrestClient,
		},
	}

	failed := false
	for _, check := range checks {
		observed, err := observedClientInformationHeader(check.exercise)
		switch {
		case err != nil:
			fmt.Printf("FAIL %s: %v\n", check.name, err)
			failed = true
		case observed != check.want:
			fmt.Printf("FAIL %s: X-Client-Info = %q, want %q\n", check.name, observed, check.want)
			failed = true
		default:
			fmt.Printf("ok   %s: X-Client-Info = %q\n", check.name, observed)
		}
	}
	if failed {
		os.Exit(1)
	}
}

// expectedVersions returns the versions the TELEMETRY_TEST_MODE environment
// variable demands of the header values, panicking on an unknown mode.
func expectedVersions() (rootVersion, postgrestVersion string) {
	mode := os.Getenv("TELEMETRY_TEST_MODE")
	switch mode {
	case "replaced-dependencies":
		return requiredRootVersion, requiredPostgrestVersion
	case "no-module-information":
		return "0.0.0", "0.0.0"
	default:
		panic(fmt.Sprintf("TELEMETRY_TEST_MODE must be %q or %q, got %q", "replaced-dependencies", "no-module-information", mode))
	}
}

// observedClientInformationHeader serves one local HTTP endpoint, lets
// exercise drive a client at it and returns the X-Client-Info header value
// received.
func observedClientInformationHeader(exercise func(baseURL string) error) (string, error) {
	var observed string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Header.Get("X-Client-Info")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("[]"))
	}))
	defer server.Close()

	if err := exercise(server.URL); err != nil {
		return "", err
	}
	return observed, nil
}

func exerciseRootClient(baseURL string) error {
	client, err := supabase.New(baseURL, "telemetry-test-key")
	if err != nil {
		return err
	}
	var rows []struct{}
	_, err = client.From("probe").Select("id").Execute(context.Background(), &rows)
	return err
}

func exercisePostgrestClient(baseURL string) error {
	client, err := postgrest.New(baseURL, "telemetry-test-key")
	if err != nil {
		return err
	}
	var rows []struct{}
	_, err = client.From("probe").Select("id").Execute(context.Background(), &rows)
	return err
}
