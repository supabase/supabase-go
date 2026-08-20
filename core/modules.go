package core

// ModulePath represents a distinct module within the Supabase Go SDK.
type ModulePath string

const basePath = "github.com/supabase/supabase-go/"

const (
	// ModulePathRoot is the import path for the Supabase Go SDK's convenience
	// entry point module, composed of other domain modules.
	ModulePathRoot ModulePath = basePath + "supabase"

	// ModulePathCore is the import path for the Supabase Go SDK's common utilities
	// that are shared between other modules.
	ModulePathCore ModulePath = basePath + "core"

	// ModulePathPostgrest is the import path for the Supabase Go SDK's PostgREST
	// client module.
	ModulePathPostgrest ModulePath = basePath + "postgrest"
)
