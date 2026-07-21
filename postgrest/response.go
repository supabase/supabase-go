package postgrest

import (
	"strconv"
	"strings"
)

// Response carries the metadata of a successfully executed query.
type Response struct {
	// HTTPStatus is the HTTP status code of the response, typically 200, or
	// 206 (Partial Content) when PostgREST serves a page of a larger result.
	HTTPStatus int

	// Count is the total number of rows matching the query when the server
	// reported one, and -1 when it did not, following the convention of
	// [net/http.Response.ContentLength].
	Count int64
}

// parseContentRangeTotal extracts the total from a PostgREST Content-Range
// header such as "0-2/3" or "*/0", returning -1 when the total is absent or
// unknown ("0-2/*", malformed, or an empty header).
func parseContentRangeTotal(contentRange string) int64 {
	_, total, found := strings.Cut(contentRange, "/")
	if !found || total == "" || total == "*" {
		return -1
	}
	parsed, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}
