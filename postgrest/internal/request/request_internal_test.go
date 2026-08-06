package request

import (
	"net/url"
	"strings"
	"testing"
)

// cSpell:ignore losslessness xfdinvalid

// TestQueryOctetSafe sweeps every possible octet against the literal safe
// set: ASCII letters and digits, RFC 3986's unreserved marks, the
// sub-delimiters that are data within a pair and the query production's
// extra characters.
func TestQueryOctetSafe(t *testing.T) {
	const safeSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789" +
		"-._~" +
		"!$'()*," +
		":@/?"

	for octet := 0; octet <= 0xFF; octet++ {
		got := queryOctetSafe(byte(octet))
		want := strings.IndexByte(safeSet, byte(octet)) >= 0
		if got != want {
			t.Errorf("queryOctetSafe(%#02x) = %t, want %t", octet, got, want)
		}
	}
}

// TestEscapeQueryComponent pins exact escaping: dialect text passes
// literally, structural and barred octets escape as uppercase hex and
// multi-byte UTF-8 escapes octet-wise.
func TestEscapeQueryComponent(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "dialect passes literally",
			input: "acquired_year.desc,name",
			want:  "acquired_year.desc,name",
		},
		{
			name:  "full literal set",
			input: "-._~!$'()*,:@/?",
			want:  "-._~!$'()*,:@/?",
		},
		{
			name:  "pair structure",
			input: "a&b=c",
			want:  "a%26b%3Dc",
		},
		{
			name:  "plus is data",
			input: "1+2",
			want:  "1%2B2",
		},
		{
			name:  "percent introduces escapes",
			input: "100%",
			want:  "100%25",
		},
		{
			name:  "hash ends a query",
			input: "a#b",
			want:  "a%23b",
		},
		{
			name:  "semicolon",
			input: "a;b",
			want:  "a%3Bb",
		},
		{
			name:  "space",
			input: "full name",
			want:  "full%20name",
		},
		{
			name:  "double quotes",
			input: `"x"`,
			want:  "%22x%22",
		},
		{
			name:  "control octets",
			input: "\x00\n\x7f",
			want:  "%00%0A%7F",
		},
		{
			name:  "hex digits render uppercase",
			input: "\xab",
			want:  "%AB",
		},
		{
			name:  "two-byte UTF-8",
			input: "é",
			want:  "%C3%A9",
		},
		{
			name:  "three-byte UTF-8",
			input: "€",
			want:  "%E2%82%AC",
		},
		{
			name:  "four-byte UTF-8 emoji",
			input: "💩",
			want:  "%F0%9F%92%A9",
		},
		{
			name:  "dialect around Unicode",
			input: `in.("💩",é)`,
			want:  "in.(%22%F0%9F%92%A9%22,%C3%A9)",
		},
		{
			name:  "invalid UTF-8 octet",
			input: "\xff",
			want:  "%FF",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := escapeQueryComponent(testCase.input); got != testCase.want {
				t.Errorf("escapeQueryComponent(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestEscapeQueryComponentRoundTripsAnyString proves losslessness: escaped
// text percent-decodes back to the exact input bytes for any byte sequence,
// valid UTF-8 or not. url.QueryUnescape is the decoding oracle; like
// PostgREST's parser it replaces + with a space, which the escaper is immune
// to because it never emits a literal +.
func TestEscapeQueryComponentRoundTripsAnyString(t *testing.T) {
	everyOctet := make([]byte, 0, 256)
	for octet := 0; octet <= 0xFF; octet++ {
		everyOctet = append(everyOctet, byte(octet))
	}

	inputs := []string{
		"a b+c%20d;e&f=g#h",
		`in.("full name",💩,é,€)`,
		"\xff\xfe\xfdinvalid",
		string(everyOctet),
	}
	for _, input := range inputs {
		decoded, err := url.QueryUnescape(escapeQueryComponent(input))
		if err != nil {
			t.Fatalf("url.QueryUnescape(escapeQueryComponent(%q)): %v", input, err)
		}
		if decoded != input {
			t.Errorf("round trip of %q = %q", input, decoded)
		}
	}
}

// TestRawQuery pins the rendering algorithm: pairs join with & in insertion
// order, keys and values escape independently and no parameters render as
// the empty string.
func TestRawQuery(t *testing.T) {
	testCases := []struct {
		name       string
		parameters []parameter
		want       string
	}{
		{
			name:       "no parameters",
			parameters: nil,
			want:       "",
		},
		{
			name:       "single pair",
			parameters: []parameter{{key: "select", value: "id"}},
			want:       "select=id",
		},
		{
			name: "insertion order preserved",
			parameters: []parameter{
				{key: "select", value: "id"},
				{key: "order", value: "name.desc,id"},
				{key: "limit", value: "1"},
			},
			want: "select=id&order=name.desc,id&limit=1",
		},
		{
			name: "repeated keys preserved",
			parameters: []parameter{
				{key: "age", value: "gte.18"},
				{key: "age", value: "lte.65"},
			},
			want: "age=gte.18&age=lte.65",
		},
		{
			name:       "empty value",
			parameters: []parameter{{key: "select", value: ""}},
			want:       "select=",
		},
		{
			name:       "empty key",
			parameters: []parameter{{key: "", value: "v"}},
			want:       "=v",
		},
		{
			name:       "key and value escape independently",
			parameters: []parameter{{key: "weird key", value: "a&b=c"}},
			want:       "weird%20key=a%26b%3Dc",
		},
		{
			name:       "Unicode value",
			parameters: []parameter{{key: "name", value: "eq.💩"}},
			want:       "name=eq.%F0%9F%92%A9",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := rawQuery(testCase.parameters); got != testCase.want {
				t.Errorf("rawQuery(%v) = %q, want %q", testCase.parameters, got, testCase.want)
			}
		})
	}
}
