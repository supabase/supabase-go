// cSpell:ignore uuid uuidfixtures

// Command uuidfixtures prints the text renderings github.com/google/uuid
// produces for the UUID fixtures used by the postgrest integration tests.
// Those tests hard-code the renderings as string literals - run this command
// to regenerate the literals or to verify them against the pinned library
// version:
//
//	GOWORK=off go -C tools/go run ./uuidfixtures
package main

import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	fmt.Println("seeded fixture identities (integration/supabase/seed.sql):")
	for _, fixture := range []string{
		"f81d4fae-7dec-11d0-a765-00a0c91e6bf6", // RFC 9562 canonical example
		"C232AB00-9414-11EC-B3C8-9F6BDECED846", // RFC 9562 UUIDv1 example
		"5df41881-3aed-3515-88a7-2f4a814cf09e", // RFC 9562 UUIDv3 example
		"919108f7-52d1-4320-9bac-f847db4148a8", // RFC 9562 UUIDv4 example
		"2ed6657d-e927-568b-95e1-2665a8aea6a2", // RFC 9562 UUIDv5 example
		"1EC9414C-232A-6B00-B3C8-9F6BDECED846", // RFC 9562 UUIDv6 example
		"017F22E2-79B0-7CC3-98C4-DC0C0C07398F", // RFC 9562 UUIDv7 example
		"2489E9AD-2EE2-8E00-8EC9-32D5F69181C0", // RFC 9562 UUIDv8 time-based example
		"5c146b14-3c52-8afd-938a-375d0df1fbf6", // RFC 9562 UUIDv8 name-based example
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", // PostgreSQL docs example
	} {
		render(`uuid.MustParse("`+fixture+`")`, uuid.MustParse(fixture))
	}

	fmt.Println()
	fmt.Println("accepted input shapes, all end up rendering to the canonical form:")
	for _, variant := range []string{
		"A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11",
		"{a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11}",
		"urn:uuid:a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"a0eebc999c0b4ef8bb6d6bb9bd380a11",
	} {
		render(variant, uuid.MustParse(variant))
	}

	fmt.Println()
	fmt.Println("other ways to obtain or construct:")
	render("uuid.Nil", uuid.Nil)
	render("uuid.Max", uuid.Max)
	render("uuid.UUID{0x12, 0x3e, ...}", uuid.UUID{
		0x12, 0x3e, 0x45, 0x67, 0xe8, 0x9b, 0x12, 0xd3,
		0xa4, 0x56, 0x42, 0x66, 0x14, 0x17, 0x40, 0x00,
	})
}

// render prints one construction alongside its String() rendering, the exact
// text a uuid.UUID operand contributes to a filter.
func render(construction string, id uuid.UUID) {
	fmt.Printf("  %-52s -> %s\n", construction, id.String())
}
