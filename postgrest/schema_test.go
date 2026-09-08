package postgrest_test

import (
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// assertProfileHeaders asserts the recorded request carried exactly the wanted
// profile headers. An empty want asserts that header's absence, so a read
// expects ("personal", "") and a write ("", "personal").
func assertProfileHeaders(t *testing.T, header http.Header, wantAcceptProfile, wantContentProfile string) {
	t.Helper()
	if got := header.Get("Accept-Profile"); got != wantAcceptProfile {
		t.Errorf("Accept-Profile = %q, want %q", got, wantAcceptProfile)
	}
	if got := header.Get("Content-Profile"); got != wantContentProfile {
		t.Errorf("Content-Profile = %q, want %q", got, wantContentProfile)
	}
}

// TestWithSchemaSendsAcceptProfileOnRead proves a read through a schema-bound
// client names the schema on Accept-Profile, the header PostgREST reads for a
// GET.
func TestWithSchemaSendsAcceptProfileOnRead(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithSchema("personal")

	if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "personal", "")
}

// TestWithSchemaSendsContentProfileOnWrites proves each write verb names the
// schema on Content-Profile, the header PostgREST reads for POST, PATCH and
// DELETE.
func TestWithSchemaSendsContentProfileOnWrites(t *testing.T) {
	testCases := []struct {
		name     string
		mutation postgrest.Mutation[map[string]any]
	}{
		{"insert", postgrest.From[map[string]any]("instruments").Insert(map[string]any{"name": "viola"})},
		{"update", postgrest.From[map[string]any]("instruments").Update(map[string]any{"name": "viola"})},
		{"delete", postgrest.From[map[string]any]("instruments").Delete()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, recorder := newRecordingServer(t)
			client := newClient(t, server.URL).WithSchema("personal")

			if _, err := postgrest.Execute(t.Context(), client, testCase.mutation); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			assertProfileHeaders(t, recorder.only(t), "", "personal")
		})
	}
}

// TestWithSchemaSendsContentProfileOnRPC proves a POST function call carries
// the schema on Content-Profile.
func TestWithSchemaSendsContentProfileOnRPC(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithSchema("personal")

	if _, _, err := postgrest.Collect(
		t.Context(), client, postgrest.RPC[map[string]any]("do_thing").Rows(),
	); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "", "personal")
}

// TestWithSchemaSendsAcceptProfileOnReadOnlyRPC proves a read-only function
// call, sent as a GET, carries the schema on Accept-Profile instead.
func TestWithSchemaSendsAcceptProfileOnReadOnlyRPC(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithSchema("personal")

	if _, _, err := postgrest.Collect(
		t.Context(), client, postgrest.RPC[map[string]any]("do_thing").Rows().ReadOnly(),
	); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "personal", "")
}

// TestWithoutSchemaSendsNoProfileHeaders proves a client with no schema set
// sends neither profile header, leaving the server's default schema in force.
func TestWithoutSchemaSendsNoProfileHeaders(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL)

	if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "", "")
}

// TestWithSchemaLeavesBaseClientUnchanged proves deriving a schema-bound client
// does not mutate the receiver: the base client still sends no profile header.
func TestWithSchemaLeavesBaseClientUnchanged(t *testing.T) {
	server, recorder := newRecordingServer(t)
	base := newClient(t, server.URL)

	_ = base.WithSchema("personal") // derive and discard

	if _, _, err := postgrest.Collect(t.Context(), base, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "", "")
}

// TestWithSchemaEmptySchemaRestoresDefault proves an empty schema clears an
// earlier selection, returning to the no-profile-header default.
func TestWithSchemaEmptySchemaRestoresDefault(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithSchema("personal").WithSchema("")

	if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "", "")
}

// TestWithSchemaReplacesEarlierSchema proves a later WithSchema wins over an
// earlier one.
func TestWithSchemaReplacesEarlierSchema(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithSchema("first").WithSchema("second")

	if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertProfileHeaders(t, recorder.only(t), "second", "")
}
