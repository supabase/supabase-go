package postgrest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNew() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(client != nil && err == nil)
	// Output: true
}

// ExampleFrom demonstrates a Database read for consumers who import
// this module directly instead of the root supabase package.
func ExampleFrom() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		var postgrestError *postgrest.Error
		if errors.As(err, &postgrestError) {
			fmt.Println(postgrestError.Code, postgrestError.Hint)
			return
		}
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments), response.HTTPStatus)
}

// ExampleCollect_schemaDriven demonstrates the dynamic escape hatch: when row
// shapes are not known at compile time, instantiate Collect with a generic
// container instead of a named struct. A bare From reads every column, which
// suits a container that names none.
func ExampleCollect_schemaDriven() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	rows, response, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.From[map[string]any]("instruments"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, row := range rows {
		fmt.Println(row["name"])
	}
	fmt.Println(response.HTTPStatus)
}

type Instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// The query is a pure value, so it may be declared once at package level,
// before any client exists, and reused across calls.
var instrumentsByName = postgrest.From[Instrument]("instruments").Select("id, name")

// ExampleFrom_packageLevel demonstrates that queries carry no client: this
// one is a package-level variable, with a client supplied only at the
// executing read function.
func ExampleFrom_packageLevel() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect(context.Background(), client, instrumentsByName)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments), response.HTTPStatus)
}

// ExampleCollect_requestTimeout demonstrates a construction-time request
// timeout influencing a read: the client abandons any request still in
// flight when its http.Client's Timeout elapses - connecting, awaiting
// headers and reading the response body all count - and Collect surfaces
// the failure as an error matching context.DeadlineExceeded, exactly as an
// expired per-request context deadline would.
func ExampleCollect_requestTimeout() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, err := postgrest.New(
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("the read did not finish within the client's timeout")
			return
		}
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments))
}

// ExampleWithRetry demonstrates the per-read override of the client-wide
// automatic-retry default set by [configuration.WithRetry]: retries are on
// by default, and a single read can opt out - or back in - without touching
// the client or the query. The contract itself - which requests qualify, on
// which failures and with what backoff - is documented on [postgrest.Client].
func ExampleWithRetry() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	// This read opts out of automatic retries, so a transient failure
	// surfaces immediately instead of after the client's re-send attempts.
	instruments, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
		postgrest.WithRetry(false),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments))
}

// ExampleCollectSingleMaybe reads a row that may legitimately be absent: the
// boolean distinguishes a missing row from a present one whose fields hold
// zero values, and more than one match fails with ErrTooManyRows.
func ExampleCollectSingleMaybe() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instrument, found, _, err := postgrest.CollectSingleMaybe(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Eq("name", "theremin"),
	)
	if err != nil {
		if errors.Is(err, postgrest.ErrTooManyRows) {
			fmt.Println("instrument names were expected to be unique")
			return
		}
		fmt.Println(err)
		return
	}
	if !found {
		fmt.Println("no such instrument")
		return
	}
	fmt.Println(instrument.ID)
}

// ExampleQueryBuilder_Insert creates rows without reading them back:
// [postgrest.Execute] applies the insert and returns only the response
// metadata, taking PostgREST's default minimal return.
func ExampleQueryBuilder_Insert() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	response, err := postgrest.Execute(
		context.Background(),
		client,
		postgrest.From[Instrument]("instruments").Insert(
			Instrument{Name: "viola"},
			Instrument{Name: "cello"},
		),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.HTTPStatus)
}

// ExampleMutationBuilder_Returning creates a row and reads it back in one call:
// passing an insert to [postgrest.Collect] returns the created rows, and
// [postgrest.MutationBuilder.Returning] narrows which columns they carry.
func ExampleMutationBuilder_Returning() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	// Insert one row and recover only the server-assigned id.
	created, _, err := postgrest.CollectSingle(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Insert(Instrument{Name: "viola"}).
			Returning("id"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(created.ID)
}

func ExampleFilterBuilder_Update() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	// A map[string]any assigns only the columns it names, leaving the rest
	// untouched. Execute applies the update without reading anything back.
	response, err := postgrest.Execute(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Eq("id", 1).
			Update(map[string]any{"name": "viola"}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.HTTPStatus)
}

// ExampleFilterBuilder_Delete removes the rows a filter chooses: the filters
// chain first, exactly as on a read, and Delete ends the chain. Execute
// applies the delete without reading anything back.
func ExampleFilterBuilder_Delete() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	response, err := postgrest.Execute(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Eq("id", 1).
			Delete(),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.HTTPStatus)
}

// ExampleQueryBuilder_Upsert creates rows, merging any that collide with an
// existing row instead of failing: [postgrest.Execute] applies the upsert and
// returns only the response metadata, taking PostgREST's default minimal
// return.
func ExampleQueryBuilder_Upsert() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	response, err := postgrest.Execute(
		context.Background(),
		client,
		postgrest.From[Instrument]("instruments").Upsert(
			Instrument{ID: 1, Name: "viola"},
			Instrument{ID: 2, Name: "cello"},
		),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.HTTPStatus)
}

// ExampleUpsertBuilder_OnConflict judges collisions on a named unique column
// rather than the primary key, and reads the affected rows back by passing the
// upsert to [postgrest.Collect].
func ExampleUpsertBuilder_OnConflict() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	rows, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Upsert(Instrument{Name: "viola"}).
			OnConflict("name"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(rows))
}

// ExampleRPCBuilder_Rows calls a table-valued Postgres function and decodes its
// rows like a table read: Arguments passes the function's inputs, Rows declares
// the result shape and Collect decodes each returned row into the named type.
func ExampleRPCBuilder_Rows() {
	type Piece struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	pieces, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			RPC[Piece]("search_pieces").
			Arguments(map[string]any{"query": "cello"}).
			Rows(),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(pieces))
}

// ExampleRPCRowsCall_ReadOnly declares a rows-returning function read-only,
// sending it as a GET. PostgREST runs a read-only call in a READ ONLY
// transaction, and the call becomes eligible for automatic retries, HTTP
// caching and Supabase read replicas - so declare ReadOnly only for a function
// that never writes, or the server refuses it.
func ExampleRPCRowsCall_ReadOnly() {
	type Piece struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	pieces, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			RPC[Piece]("search_pieces").
			Arguments(map[string]any{"query": "cello"}).
			Rows().
			ReadOnly(),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(pieces))
}

// ExampleRPCBuilder_Value calls a function returning one JSON value and decodes
// the whole body with CollectRaw. This function takes no arguments, so Arguments
// is omitted and the call runs on the function's own parameter defaults.
func ExampleRPCBuilder_Value() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	count, _, err := postgrest.CollectRaw(
		context.Background(),
		client,
		postgrest.RPC[int]("count_pieces").Value(),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(count)
}

// ExampleRPCVoid calls a function that returns nothing. RPCVoid needs no result
// type and no result shape: the call is ready for Execute, which runs it and
// reads nothing back.
func ExampleRPCVoid() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	response, err := postgrest.Execute(
		context.Background(),
		client,
		postgrest.RPCVoid("refresh_reporting_view"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(response.HTTPStatus)
}

// ExampleClient_WithAccessTokenProvider demonstrates acting for a signed-in end
// user: the derived client resolves and sends the user's access token so the
// database applies that user's Row Level Security policies, while the base
// client keeps authenticating with the project API key alone. A token already
// in hand rides a constant provider, as here; a rotating token would be read
// from wherever the application keeps it current.
func ExampleClient_WithAccessTokenProvider() {
	type PracticeLog struct {
		Piece   string `json:"piece"`
		Minutes int    `json:"minutes"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	accessToken := "USER_ACCESS_TOKEN"
	userClient := client.WithAccessTokenProvider(
		func(context.Context) (string, error) { return accessToken, nil },
	)
	logs, _, err := postgrest.Collect(
		context.Background(),
		userClient,
		postgrest.From[PracticeLog]("practice_logs"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(logs))
}
