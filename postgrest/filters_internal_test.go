package postgrest

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

var (
	spookyTime          = time.Date(2026, time.October, 31, 20, 0, 0, 0, time.UTC)
	newYorkTimezone     = time.FixedZone("EDT", -4*60*60)
	spookyTimeInNewYork = spookyTime.In(newYorkTimezone)
)

type team struct {
	Name        string
	City        string
	PlayerNames []string
}

// String implements [fmt.Stringer] for testing purposes.
func (t team) String() string {
	return fmt.Sprintf("%s %s\nRoster: %s",
		t.City,
		t.Name,
		strings.Join(t.PlayerNames, ", "))
}

var gopherTeam = team{
	Name: "Gophers",
	City: "GoLand",
	PlayerNames: []string{
		"Alice",
		"Bob",
		"Charlie",
	},
}

var virtualRouterRedundancyProtocolMAC, _ = net.ParseMAC("00:00:5e:00:01:01")

func TestRenderFilterValue(t *testing.T) {
	testCases := []struct {
		value any
		want  string
	}{
		{nil, "null"},

		{"", ""},
		{" ", " "},
		{"\n", "\n"},
		{"Hello\tWorld", "Hello\tWorld"},
		{"你好世界", "你好世界"},

		{true, "true"},
		{false, "false"},

		{-1, "-1"},
		{0, "0"},
		{1, "1"},
		{uint(42), "42"},
		{'a', "97"},     // U+0061
		{'π', "960"},    // U+03C0
		{'众', "20247"},  // U+4F17
		{'💩', "128169"}, // U+1F4A9
		{int8(0), "0"},
		{int16(0), "0"},
		{int32(0), "0"},
		{int64(0), "0"},
		{uint8(0), "0"},
		{uint16(0), "0"},
		{uint32(0), "0"},
		{uint64(0), "0"},
		{int8(math.MinInt8), "-128"},
		{int16(math.MinInt16), "-32768"},
		{int32(math.MinInt32), "-2147483648"},
		{int64(math.MinInt64), "-9223372036854775808"},
		{int8(math.MaxInt8), "127"},
		{int16(math.MaxInt16), "32767"},
		{int32(math.MaxInt32), "2147483647"},
		{int64(math.MaxInt64), "9223372036854775807"},
		{uint8(math.MaxUint8), "255"},
		{uint16(math.MaxUint16), "65535"},
		{uint32(math.MaxUint32), "4294967295"},
		{uint64(math.MaxUint64), "18446744073709551615"},
		{float32(math.MaxFloat32), "3.4028235e+38"},
		{float32(math.SmallestNonzeroFloat32), "1e-45"},
		{float64(math.MaxFloat64), "1.7976931348623157e+308"},
		{float64(math.SmallestNonzeroFloat64), "5e-324"},
		{1.5, "1.5"},
		{-273.15, "-273.15"},
		{float32(0.1), "0.1"},       // shortest for the float32 bits, not 0.100000001490116...
		{float64(3), "3"},           // integral floats drop the point entirely, no "3.0"
		{float64(1000000), "1e+06"}, // 'g' switches to exponent form at 1e6
		{float64(0.0001), "0.0001"}, // ...and below 1e-4
		{float64(0.00001), "1e-05"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{float32(math.NaN()), "NaN"},
		{float32(math.Inf(1)), "Infinity"},
		{float32(math.Inf(-1)), "-Infinity"},

		{spookyTime, "2026-10-31T20:00:00Z"},
		{spookyTimeInNewYork, "2026-10-31T16:00:00-04:00"},
		{spookyTime.Add(123456789 * time.Nanosecond), "2026-10-31T20:00:00.123456789Z"},
		{spookyTime.Add(500 * time.Millisecond), "2026-10-31T20:00:00.5Z"}, // .5, not .500
		{time.Time{}, "0001-01-01T00:00:00Z"},                              // the uninitialized-field accident, rendered not panicking

		{(*int)(nil), "<nil>"},        // typed nil is not untyped nil: only the latter renders null
		{[]string{"a", "b"}, "[a b]"}, // slices are not expanded: In wants variadic values, not one slice

		// []byte renders as PostgreSQL's bytea hex format
		{[]byte(nil), `\x`},
		{[]byte{0}, `\x00`},
		{[]byte("hello"), `\x68656c6c6f`},
		{[]byte{0xDE, 0xAD, 0xBE, 0xEF}, `\xdeadbeef`},

		// via fmt.Stringer
		{90 * time.Minute, "1h30m0s"},
		{time.Month(8), "August"},
		{json.Number("19.99"), "19.99"},
		{gopherTeam, "GoLand Gophers\nRoster: Alice, Bob, Charlie"},
		{net.IPv4(8, 8, 8, 8), "8.8.8.8"},
		{net.IPv4(127, 0, 0, 1), "127.0.0.1"},
		{net.ParseIP("2001:4860:4860::8888"), "2001:4860:4860::8888"},
		{net.ParseIP("::1"), "::1"},
		{net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, "ff:ff:ff:ff:ff:ff"},
		{virtualRouterRedundancyProtocolMAC, "00:00:5e:00:01:01"},
		{&url.URL{Scheme: "https", Host: "www.example.com", Path: "/about"}, "https://www.example.com/about"},
		{&url.URL{Scheme: "https", Host: "github.com", Path: "/golang/go", RawQuery: "tab=readme-ov-file"}, "https://github.com/golang/go?tab=readme-ov-file"},

		// via fmt.Stringer: a Range renders as its PostgreSQL range literal
		{NewRange[int]().FromInclusive(2).ToExclusive(7), "[2,7)"},
		{EmptyRange(), "empty"},
	}

	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("%T:%v", testCase.value, testCase.value), func(t *testing.T) {
			if got := renderFilterValue(testCase.value); got != testCase.want {
				t.Errorf("renderFilterValue(%#v) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

// looksLikeUglyStructRepresentation inspects the string that was got to
// ascertain whether it looks like the ugly formatting we get for a struct
// when it doesn't provide a pretty formatted fmt.Stringer implementation.
// For example, "{https   www.example.com /about     false false}" for
// url.URL{Scheme: "https", Host: "www.example.com", Path: "/about"}.
func looksLikeUglyStructRepresentation(got string) bool {
	return strings.HasPrefix(got, "{") && strings.HasSuffix(got, "}")
}

func TestRenderFilterValueDefaultUgly(t *testing.T) {
	testCases := []struct {
		value   any
		checker func(got string) bool
	}{
		{url.URL{Scheme: "https", Host: "www.example.com", Path: "/about"}, looksLikeUglyStructRepresentation},
		{url.URL{Scheme: "https", Host: "github.com", Path: "/golang/go", RawQuery: "tab=readme-ov-file"}, looksLikeUglyStructRepresentation},
	}

	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("%T:%v", testCase.value, testCase.value), func(t *testing.T) {
			if got := renderFilterValue(testCase.value); !testCase.checker(got) {
				t.Errorf("renderFilterValue(%#v) = %v", testCase.value, got)
			}
		})
	}
}
