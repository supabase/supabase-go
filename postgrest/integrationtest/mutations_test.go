//go:build integration

package integrationtest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// repertoirePiece maps the row type of the public.repertoire table.
// The id and difficulty tags carry omitzero so an insert that names neither
// lets the server assign the identity and apply the difficulty default.
type repertoirePiece struct {
	ID         int    `json:"id,omitzero"`
	Title      string `json:"title"`
	Composer   string `json:"composer,omitzero"`
	Difficulty int    `json:"difficulty,omitzero"`
}

// suggestion maps the row type of the public.suggestion_box table, whose id
// is an ordinary primary key the caller supplies.
type suggestion struct {
	ID         int    `json:"id"`
	Suggestion string `json:"suggestion"`
}

// TestInsertMinimalReturnsNoRepresentation proves an Execute insert against
// real PostgREST: the write applies with a 201 and no body, and a follow-up
// read finds the row with its server-assigned id and the defaulted difficulty.
func TestInsertMinimalReturnsNoRepresentation(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Minimal insert returns no representation"

	response, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Glass"}),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}

	row, _, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Eq("title", title),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if row.ID == 0 {
		t.Error("id = 0, want a server-assigned identity")
	}
	if row.Composer != "Glass" {
		t.Errorf("composer = %q, want Glass", row.Composer)
	}
	if row.Difficulty != 3 {
		t.Errorf("difficulty = %d, want the default 3", row.Difficulty)
	}
}

// TestInsertReturningRepresentation proves the issue's headline capability
// against real PostgREST: a plain Collect over an insert returns exactly the
// created row, carrying the server-assigned id and the defaulted difficulty -
// exact fields, not merely a status.
func TestInsertReturningRepresentation(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Insert returning representation"

	created, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Bridge"}),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
	if len(created) != 1 {
		t.Fatalf("created %d rows, want 1", len(created))
	}
	row := created[0]
	if row.ID == 0 {
		t.Error("id = 0, want a server-assigned identity")
	}
	if row.Title != title || row.Composer != "Bridge" {
		t.Errorf("row = %+v, want title %q composer Bridge", row, title)
	}
	if row.Difficulty != 3 {
		t.Errorf("difficulty = %d, want the default 3", row.Difficulty)
	}
}

// TestInsertBulkReturningAll proves a bulk insert returns every created row,
// in insertion order, with server-assigned identities. Every row names the
// same columns on purpose: a bulk insert must send a uniform key set, and
// omitzero would drop a zero-valued field from some rows but not others - the
// ragged batch the server rejects in TestInsertNonUniformBulkKeysRejected.
func TestInsertBulkReturningAll(t *testing.T) {
	client := newIntegrationClient(t)

	pieces := []repertoirePiece{
		{Title: "Bulk piece one", Composer: "Glass", Difficulty: 5},
		{Title: "Bulk piece two", Composer: "Bridge", Difficulty: 8},
		{Title: "Bulk piece three", Composer: "Price", Difficulty: 2},
	}
	created, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(pieces...),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
	if len(created) != 3 {
		t.Fatalf("created %d rows, want 3", len(created))
	}
	for index, want := range pieces {
		got := created[index]
		if got.ID == 0 {
			t.Errorf("created[%d].id = 0, want a server-assigned identity", index)
		}
		if got.Title != want.Title || got.Composer != want.Composer || got.Difficulty != want.Difficulty {
			t.Errorf("created[%d] = %+v, want title %q composer %q difficulty %d",
				index, got, want.Title, want.Composer, want.Difficulty)
		}
	}
}

// TestInsertProjectedRepresentation proves Returning narrows the representation
// against real PostgREST: with Returning("title") the created row comes back
// carrying only its title, so the difficulty the server defaulted to 3 never
// crosses the wire and decodes as zero.
func TestInsertProjectedRepresentation(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Projected representation"

	type titleAndDifficulty struct {
		Title      string `json:"title"`
		Difficulty int    `json:"difficulty,omitzero"`
	}
	created, _, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[titleAndDifficulty]("repertoire").
			Insert(titleAndDifficulty{Title: title}).
			Returning("title"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d rows, want 1", len(created))
	}
	if created[0].Title != title {
		t.Errorf("title = %q, want %q", created[0].Title, title)
	}
	if created[0].Difficulty != 0 {
		t.Errorf("difficulty = %d, want 0 (Returning(\"title\") excluded it)", created[0].Difficulty)
	}
}

// TestInsertReturningSingleRow proves CollectSingle over a one-row insert
// against real PostgREST: the singular media type rides the POST and the lone
// created row decodes directly, no array wrapper involved.
func TestInsertReturningSingleRow(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Insert returning single row"

	row, response, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Price"}),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
	if row.ID == 0 || row.Title != title {
		t.Errorf("row = %+v, want title %q with a server-assigned id", row, title)
	}
}

// TestInsertWithoutSelectPermission proves the documented RLS trap against real
// PostgREST as purely an execution-layer difference: the same builder fails
// through Collect, which asks for a representation the anon role may not SELECT
// (42501, rolling back), yet succeeds through Execute, which asks for no rows.
func TestInsertWithoutSelectPermission(t *testing.T) {
	client := newIntegrationClient(t)

	builder := postgrest.
		From[suggestion]("suggestion_box").
		Insert(suggestion{ID: 1, Suggestion: "A fine suggestion"})

	_, _, err := postgrest.Collect(t.Context(), client, builder)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("Collect: want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "42501" {
		t.Errorf("Code = %q, want 42501 (a returned representation needs SELECT rights)", typedError.Code)
	}

	response, err := postgrest.Execute(t.Context(), client, builder)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
}

// TestInsertNonUniformBulkKeysRejected proves the server rules on a ragged
// batch: bulk rows whose keys differ are rejected with PGRST102, the SDK
// sending them verbatim rather than computing a client-side column union.
func TestInsertNonUniformBulkKeysRejected(t *testing.T) {
	client := newIntegrationClient(t)

	_, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[map[string]any]("repertoire").Insert(
			map[string]any{"title": "Ragged one", "composer": "Glass"},
			map[string]any{"title": "Ragged two", "difficulty": 5},
		),
	)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST102" {
		t.Errorf("Code = %q, want PGRST102 (all bulk rows must share one key set)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", typedError.HTTPStatus)
	}
}

// TestInsertDuplicateKeyConflict proves a unique-constraint violation surfaces
// as a typed error against real PostgREST: a second insert of a title already
// present fails with 23505 and a populated Details naming the conflict.
func TestInsertDuplicateKeyConflict(t *testing.T) {
	client := newIntegrationClient(t)

	builder := postgrest.
		From[repertoirePiece]("repertoire").
		Insert(repertoirePiece{Title: "Duplicate key conflict", Composer: "Bridge"})
	if _, err := postgrest.Execute(t.Context(), client, builder); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	_, err := postgrest.Execute(t.Context(), client, builder)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("second insert: want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "23505" {
		t.Errorf("Code = %q, want 23505 (unique violation on title)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusConflict {
		t.Errorf("HTTPStatus = %d, want 409", typedError.HTTPStatus)
	}
}

// TestUpdateFilteredRowReturningRepresentation proves a filtered update through
// Collect against real PostgREST: after seeding a row, an update behind an Eq
// filter returns exactly the affected row carrying the changed column, at 200.
func TestUpdateFilteredRowReturningRepresentation(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Update filtered row returning representation"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Glass"}),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	updated, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[repertoirePiece]("repertoire").
			Eq("title", title).
			Update(map[string]any{"composer": "Bridge"}),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if len(updated) != 1 {
		t.Fatalf("updated %d rows, want 1", len(updated))
	}
	if updated[0].Title != title || updated[0].Composer != "Bridge" {
		t.Errorf("row = %+v, want title %q composer Bridge", updated[0], title)
	}
}

// TestUpdateClearsColumnWithExplicitNull proves the map[string]any null contract
// against real PostgREST: a key carrying nil clears its column to SQL null,
// which a pointer field in the returned representation decodes as nil.
func TestUpdateClearsColumnWithExplicitNull(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Update clears column with explicit null"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Glass"}),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	type nullableComposer struct {
		Title    string  `json:"title"`
		Composer *string `json:"composer"`
	}
	updated, _, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.
			From[nullableComposer]("repertoire").
			Eq("title", title).
			Update(map[string]any{"composer": nil}),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if updated.Composer != nil {
		t.Errorf("composer = %q, want nil (cleared to SQL null)", *updated.Composer)
	}
}

// TestUpdateMinimal proves an Execute update against real PostgREST: the change
// applies with a 204 and no body, and a follow-up read sees the new value.
func TestUpdateMinimal(t *testing.T) {
	client := newIntegrationClient(t)
	title := "Update minimal"

	if _, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Insert(repertoirePiece{Title: title, Composer: "Glass", Difficulty: 4}),
	); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	response, err := postgrest.Execute(
		t.Context(),
		client,
		postgrest.
			From[repertoirePiece]("repertoire").
			Eq("title", title).
			Update(map[string]any{"difficulty": 9}),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusNoContent {
		t.Errorf("HTTPStatus = %d, want 204", response.HTTPStatus)
	}

	row, _, err := postgrest.CollectSingle(
		t.Context(),
		client,
		postgrest.From[repertoirePiece]("repertoire").Eq("title", title),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if row.Difficulty != 9 {
		t.Errorf("difficulty = %d, want 9 after the update", row.Difficulty)
	}
}

// TestUpdateMatchingNoRows proves an update whose filter matches nothing is an
// ordinary empty result against real PostgREST: Collect returns an empty slice
// at 200, not an error.
func TestUpdateMatchingNoRows(t *testing.T) {
	client := newIntegrationClient(t)

	updated, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[repertoirePiece]("repertoire").
			Eq("title", "No such title ever inserted by any test").
			Update(map[string]any{"composer": "Nobody"}),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if len(updated) != 0 {
		t.Errorf("updated %d rows, want 0 (filter matched nothing)", len(updated))
	}
}
