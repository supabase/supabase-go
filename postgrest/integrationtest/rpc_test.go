package integrationtest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// repertoireContains reports whether rows carry a piece with the given title,
// letting a function test assert on its own seeded rows without depending on the
// shared table being otherwise empty.
func repertoireContains(rows []repertoirePiece, title string) bool {
	for _, row := range rows {
		if row.Title == title {
			return true
		}
	}
	return false
}

// TestRPCRowsSetReturning proves a table-valued function decodes like a table
// read: repertoire_easier_than returns a set of repertoire rows through Rows()
// and Collect, carrying this test's below-threshold seed and excluding its
// above-threshold one.
func TestRPCRowsSetReturning(t *testing.T) {
	client := newIntegrationClient(t)
	easy := "RPC rows set returning easy"
	hard := "RPC rows set returning hard"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(
			repertoirePiece{Title: easy, Composer: "Glass", Difficulty: 2},
			repertoirePiece{Title: hard, Composer: "Price", Difficulty: 9},
		),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.RPC[repertoirePiece]("repertoire_easier_than").Arguments(map[string]any{"max_difficulty": 5}).Rows(),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if !repertoireContains(rows, easy) {
		t.Errorf("result missing %q, want it present (difficulty 2 is under the threshold)", easy)
	}
	if repertoireContains(rows, hard) {
		t.Errorf("result contains %q, want it absent (difficulty 9 is over the threshold)", hard)
	}
}

// TestRPCRowsReadOnlyViaGet proves the read-only rows form calls the function
// over GET end to end: repertoire_easier_than is stable, so it runs under the
// READ ONLY transaction a GET opens and its rows still decode through Collect.
func TestRPCRowsReadOnlyViaGet(t *testing.T) {
	client := newIntegrationClient(t)
	easy := "RPC rows read-only via get"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: easy, Composer: "Bridge", Difficulty: 2}),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	rows, _, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.RPC[repertoirePiece]("repertoire_easier_than").Arguments(map[string]any{"max_difficulty": 5}).Rows().ReadOnly(),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !repertoireContains(rows, easy) {
		t.Errorf("result missing %q, want it present through the read-only GET call", easy)
	}
}

// TestRPCValueScalar proves a scalar-returning function decodes whole through
// Value() and CollectRaw: add_them answers a bare 3, handed back with no row
// wrapper.
func TestRPCValueScalar(t *testing.T) {
	client := newIntegrationClient(t)

	sum, response, err := postgrest.CollectRaw(
		t.Context(),
		client,
		postgrest.RPC[int]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Value(),
	)
	if err != nil {
		t.Fatalf("CollectRaw: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if sum != 3 {
		t.Errorf("sum = %d, want 3", sum)
	}
}

// TestRPCValueReadOnlyScalar proves the read-only value form calls the scalar
// function over GET: add_them is immutable, so it runs read-only and still
// answers 3, its arguments carried in the query string.
func TestRPCValueReadOnlyScalar(t *testing.T) {
	client := newIntegrationClient(t)

	sum, _, err := postgrest.CollectRaw(
		t.Context(),
		client,
		postgrest.RPC[int]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Value().ReadOnly(),
	)
	if err != nil {
		t.Fatalf("CollectRaw: %v", err)
	}
	if sum != 3 {
		t.Errorf("sum = %d, want 3", sum)
	}
}

// TestRPCValueSingleUnnamedJSONParameter proves the single-unnamed-parameter
// call from both sides of PostgREST's payload rule: the arguments become the
// whole request body, and an array body is accepted only when every element is
// an object carrying one shared key set.
func TestRPCValueSingleUnnamedJSONParameter(t *testing.T) {
	client := newIntegrationClient(t)

	t.Run("array of uniform objects reaches the parameter whole", func(t *testing.T) {
		sum, _, err := postgrest.CollectRaw(
			t.Context(),
			client,
			postgrest.RPC[int]("sum_json").Arguments([]map[string]int{{"value": 3}, {"value": 4}}).Value(),
		)
		if err != nil {
			t.Fatalf("CollectRaw: %v", err)
		}
		if sum != 7 {
			t.Errorf("sum = %d, want 7", sum)
		}
	})

	t.Run("array of bare scalars is rejected by the payload rule", func(t *testing.T) {
		_, _, err := postgrest.CollectRaw(
			t.Context(),
			client,
			postgrest.RPC[int]("sum_json").Arguments([]int{3, 4}).Value(),
		)
		var typedError *postgrest.Error
		if !errors.As(err, &typedError) {
			t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
		}
		if typedError.Code != "PGRST102" {
			t.Errorf("Code = %q, want PGRST102 (array elements must share one key set)", typedError.Code)
		}
	})
}

// TestRPCDefaultParameterOmitted proves omitting Arguments sends the empty
// object, so a defaulted parameter takes its default: greet answers "hello
// world" with no name supplied.
func TestRPCDefaultParameterOmitted(t *testing.T) {
	client := newIntegrationClient(t)

	greeting, _, err := postgrest.CollectRaw(
		t.Context(),
		client,
		postgrest.RPC[string]("greet").Value(),
	)
	if err != nil {
		t.Fatalf("CollectRaw: %v", err)
	}
	if greeting != "hello world" {
		t.Errorf("greeting = %q, want %q", greeting, "hello world")
	}
}

// TestRPCVoidFunction proves a void function applies through RPCVoid and
// Execute: the call returns 204 with no body to decode.
func TestRPCVoidFunction(t *testing.T) {
	client := newIntegrationClient(t)

	response, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.RPCVoid("void_function"),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusNoContent {
		t.Errorf("HTTPStatus = %d, want 204", response.HTTPStatus)
	}
}

// TestRPCVolatileMutatesAndPersists proves a volatile function's writes persist:
// raise_difficulty bumps this test's row through Rows() and returns it, and a
// read-back confirms the change landed.
func TestRPCVolatileMutatesAndPersists(t *testing.T) {
	client := newIntegrationClient(t)
	title := "RPC volatile mutates and persists"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Glass", Difficulty: 3}),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	bumped, _, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.RPC[repertoirePiece]("raise_difficulty").Arguments(map[string]any{"piece_title": title}).Rows(),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if bumped.Difficulty != 4 {
		t.Errorf("returned difficulty = %d, want 4 after the bump", bumped.Difficulty)
	}

	readBack, _, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Eq("title", title),
	)
	if err != nil {
		t.Fatalf("read-back CollectSingle: %v", err)
	}
	if readBack.Difficulty != 4 {
		t.Errorf("persisted difficulty = %d, want 4", readBack.Difficulty)
	}
}

// TestRPCWriteRejectedUnderReadOnly proves the read-only declaration cannot lie
// its way into damage: raise_difficulty writes, so calling it through
// Rows().ReadOnly() runs it in a READ ONLY transaction the server refuses,
// surfacing as PostgreSQL's read_only_sql_transaction (25006) mapped to 405.
func TestRPCWriteRejectedUnderReadOnly(t *testing.T) {
	client := newIntegrationClient(t)

	_, _, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.RPC[repertoirePiece]("raise_difficulty").
			Arguments(map[string]any{"piece_title": "RPC write rejected under read only"}).
			Rows().
			ReadOnly(),
	)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "25006" {
		t.Errorf("Code = %q, want 25006 (read_only_sql_transaction)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusMethodNotAllowed {
		t.Errorf("HTTPStatus = %d, want 405", typedError.HTTPStatus)
	}
}

// TestRPCUnknownFunction proves an unknown function surfaces as a typed error:
// PostgREST answers a missing rpc target with PGRST202 at HTTP 404.
func TestRPCUnknownFunction(t *testing.T) {
	client := newIntegrationClient(t)

	_, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.RPCVoid("no_such_function_ever_defined"),
	)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST202" {
		t.Errorf("Code = %q, want PGRST202 (unknown function)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", typedError.HTTPStatus)
	}
}
