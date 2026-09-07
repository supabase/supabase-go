package postgrest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// rpcPathSegment is the fixed first path segment PostgREST mounts its function
// endpoint under, so a call addresses rpc/<function>.
const rpcPathSegment = "rpc"

// RPC begins a call of the named Postgres function whose result decodes into T.
// The call cannot execute until a result shape is chosen: [RPCBuilder.Rows] for
// a function returning a set of rows, decoded one element into T by [Collect]
// and its kin, or [RPCBuilder.Value] for a function returning one JSON value,
// decoded whole into T by [CollectRaw]. Chain [RPCBuilder.Arguments] first to
// pass inputs. For a function returning nothing, use [RPCVoid] instead.
//
// The returned builder is a pure immutable value carrying no client, exactly
// like [From]'s, so a call may be declared once and executed later.
func RPC[T any](function string) RPCBuilder[T] {
	return RPCBuilder[T]{function: function}
}

// RPCVoid begins a call of a Postgres function that returns nothing, ready for
// [Execute] with no result shape to declare. Chain [RPCVoidCall.Arguments] to
// pass inputs. For a function that returns rows or a value, use [RPC] instead.
func RPCVoid(function string) RPCVoidCall {
	return RPCVoidCall{function: function}
}

// RPCBuilder is a function call still awaiting its result shape. [RPCBuilder.Rows]
// and [RPCBuilder.Value] declare the shape and yield an executable call.
// [RPCBuilder.Arguments] passes the inputs. The builder itself cannot execute,
// since the shape governs how the result decodes.
// An RPCBuilder is an immutable value.
type RPCBuilder[T any] struct {
	function  string
	arguments any
}

// Arguments passes inputs to the function. The value marshals with
// [encoding/json], so a map[string]any or a tagged struct produces the named
// parameters PostgREST reads, while an array or bare value produces the
// single-unnamed-parameter call. PostgREST accepts an array body only when
// every element is an object carrying one shared key set, rejecting others
// with code PGRST102. Omitting Arguments runs the function on its parameter
// defaults. A later Arguments replaces an earlier one.
func (r RPCBuilder[T]) Arguments(arguments any) RPCBuilder[T] {
	r.arguments = arguments
	return r
}

// Rows declares that the function returns a set of rows, decoded one element
// into T by [Collect] and its kin exactly as a table read is. The call is sent
// as a POST, which runs every function, so chain [RPCRowsCall.ReadOnly] when
// the function only reads. Passing the returned call to [Execute] runs the
// function and discards its rows.
func (r RPCBuilder[T]) Rows() RPCRowsCall[T] {
	return RPCRowsCall[T](r)
}

// Value declares that the function returns one JSON value - a scalar such as 3
// or "ONLINE", an object or an array - decoded whole into T by [CollectRaw].
// The call is sent as a POST, which runs every function, so chain
// [RPCValueCall.ReadOnly] when the function only reads.
func (r RPCBuilder[T]) Value() RPCValueCall[T] {
	return RPCValueCall[T](r)
}

// RPCRowsCall is a function call declared to return rows, sent as a POST so it
// runs every function. Pass it to [Collect] and its kin to decode the rows, to
// [CollectRaw] to decode the whole result array, or to [Execute] to run it and
// discard the rows. Chain [RPCRowsCall.ReadOnly] to declare the function
// read-only and send it as a GET instead.
// An RPCRowsCall is an immutable value.
type RPCRowsCall[T any] struct {
	function  string
	arguments any
}

// ReadOnly declares that the function only reads, sending the call as a GET
// whose arguments travel in the query string. PostgREST runs a GET in a READ
// ONLY transaction, so a function that writes fails rather than the declaration
// taking effect, and the call becomes eligible for automatic retries, HTTP
// caching and Supabase read replicas. The GET carries no request body, so it
// cannot run through [Execute].
func (c RPCRowsCall[T]) ReadOnly() RPCReadOnlyRowsCall[T] {
	return RPCReadOnlyRowsCall[T](c)
}

func (c RPCRowsCall[T]) postState() queryState[T] {
	return queryState[T]{request: rpcPostRequest(c.function, c.arguments)}
}

// state implements [Query].
func (c RPCRowsCall[T]) state() queryState[T] { return c.postState() }

// rawState implements [RawQuery].
func (c RPCRowsCall[T]) rawState() queryState[T] { return c.postState() }

// executeState implements [Mutation].
func (c RPCRowsCall[T]) executeState() queryState[T] { return c.postState() }

// mutation implements [Mutation].
func (c RPCRowsCall[T]) mutation() {}

// RPCReadOnlyRowsCall is a rows-returning function call declared read-only,
// sent as a GET that PostgREST runs in a READ ONLY transaction. Pass it to
// [Collect] and its kin to decode the rows, or to [CollectRaw] to decode the
// whole result array. Being a GET, the call qualifies for automatic retries.
// An RPCReadOnlyRowsCall is an immutable value.
type RPCReadOnlyRowsCall[T any] struct {
	function  string
	arguments any
}

func (c RPCReadOnlyRowsCall[T]) getState() queryState[T] {
	return queryState[T]{request: rpcGetRequest(c.function, c.arguments)}
}

// state implements [Query].
func (c RPCReadOnlyRowsCall[T]) state() queryState[T] { return c.getState() }

// rawState implements [RawQuery].
func (c RPCReadOnlyRowsCall[T]) rawState() queryState[T] { return c.getState() }

// RPCValueCall is a function call declared to return one JSON value, sent as a
// POST so it runs every function. Pass it to [CollectRaw] to decode the whole
// body into T. Chain [RPCValueCall.ReadOnly] to declare the function read-only
// and send it as a GET instead.
// An RPCValueCall is an immutable value.
type RPCValueCall[T any] struct {
	function  string
	arguments any
}

// ReadOnly declares that the function only reads, sending the call as a GET
// whose arguments travel in the query string. PostgREST runs a GET in a READ
// ONLY transaction, so a function that writes fails rather than the declaration
// taking effect, and the call becomes eligible for automatic retries, HTTP
// caching and Supabase read replicas.
func (c RPCValueCall[T]) ReadOnly() RPCReadOnlyValueCall[T] {
	return RPCReadOnlyValueCall[T](c)
}

// rawState implements [RawQuery].
func (c RPCValueCall[T]) rawState() queryState[T] {
	return queryState[T]{request: rpcPostRequest(c.function, c.arguments)}
}

// RPCReadOnlyValueCall is a value-returning function call declared read-only,
// sent as a GET that PostgREST runs in a READ ONLY transaction. Pass it to
// [CollectRaw] to decode the whole body into T. Being a GET, the call qualifies
// for automatic retries.
// An RPCReadOnlyValueCall is an immutable value.
type RPCReadOnlyValueCall[T any] struct {
	function  string
	arguments any
}

// rawState implements [RawQuery].
func (c RPCReadOnlyValueCall[T]) rawState() queryState[T] {
	return queryState[T]{request: rpcGetRequest(c.function, c.arguments)}
}

// RPCVoidCall is a call of a function that returns nothing, ready for [Execute],
// which runs it and decodes no body. It carries no result shape and no
// read-only form: a call that returns nothing has only "run it".
// An RPCVoidCall is an immutable value.
type RPCVoidCall struct {
	function  string
	arguments any
}

// Arguments passes inputs to the function, exactly as [RPCBuilder.Arguments]
// does. Omitting Arguments runs the function on its parameter defaults, and a
// later Arguments replaces an earlier one.
func (c RPCVoidCall) Arguments(arguments any) RPCVoidCall {
	c.arguments = arguments
	return c
}

// executeState implements [Mutation]. RPCVoidCall carries no result type, so it
// satisfies Mutation[struct{}], and [Execute] infers that type parameter with
// no ceremony at the call site.
func (c RPCVoidCall) executeState() queryState[struct{}] {
	return queryState[struct{}]{request: rpcPostRequest(c.function, c.arguments)}
}

// mutation implements [Mutation].
func (c RPCVoidCall) mutation() {}

// rpcPostRequest builds the POST form of a function call: the arguments as the
// JSON request body, or the empty object when none were given so the function
// runs on its parameter defaults. A marshalling failure is parked as a deferred
// build error, surfacing at the executing function rather than on the wire.
func rpcPostRequest(function string, arguments any) request.Request {
	post := request.New(http.MethodPost, rpcPathSegment, function)
	if arguments == nil {
		return post.WithBody([]byte("{}"))
	}
	body, err := json.Marshal(arguments)
	if err != nil {
		return post.WithError(fmt.Errorf("postgrest: encoding rpc arguments: %w", err))
	}
	return post.WithBody(body)
}

// rpcGetRequest builds the read-only GET form of a function call: each key of
// the arguments object becomes a query parameter, so PostgREST runs the
// function in a READ ONLY transaction. No arguments sends a bare query.
// Arguments that do not marshal to a JSON object park a deferred build error,
// since only an object maps onto named query parameters.
func rpcGetRequest(function string, arguments any) request.Request {
	get := request.New(http.MethodGet, rpcPathSegment, function)
	if arguments == nil {
		return get
	}
	object, err := rpcArgumentsObject(arguments)
	if err != nil {
		return get.WithError(err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		get = get.WithParameter(key, rpcQueryValue(object[key]))
	}
	return get
}

// rpcArgumentsObject renders arguments as a JSON object so its keys can travel
// as query parameters. A value that does not encode to an object - an array or
// a scalar - yields an error naming the read-only requirement.
func rpcArgumentsObject(arguments any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("postgrest: encoding rpc arguments: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, fmt.Errorf("postgrest: read-only rpc arguments must be a JSON object: %w", err)
	}
	return object, nil
}

// rpcQueryValue renders one argument value as its query-parameter text: a JSON
// string bare, a JSON array in PostgreSQL's {1,2,3} array-literal form
// (supabase-js parity) and anything else as its JSON text.
func rpcQueryValue(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return text
		}
	case '[':
		var elements []json.RawMessage
		if err := json.Unmarshal(trimmed, &elements); err == nil {
			return rpcArrayLiteral(elements)
		}
	}
	return string(trimmed)
}

// rpcArrayLiteral renders a JSON array as PostgreSQL's {a,b,c} array literal,
// each element rendered as rpcQueryValue renders it.
func rpcArrayLiteral(elements []json.RawMessage) string {
	var builder strings.Builder
	builder.WriteByte('{')
	for index, element := range elements {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(rpcQueryValue(element))
	}
	builder.WriteByte('}')
	return builder.String()
}

// Compile-time proof that each RPC call type satisfies exactly the sealed
// interfaces its result shape and transport allow. A drift that
// added a forbidden capability - a Value call decoding rows, a read-only call
// reaching [Execute], a void call reaching [Collect] - would fail to compile a
// line below rather than at a distant call site.
var (
	_ Query[any]         = RPCRowsCall[any]{}
	_ Mutation[any]      = RPCRowsCall[any]{}
	_ Query[any]         = RPCReadOnlyRowsCall[any]{}
	_ RawQuery[any]      = RPCValueCall[any]{}
	_ RawQuery[any]      = RPCReadOnlyValueCall[any]{}
	_ Mutation[struct{}] = RPCVoidCall{}
)
