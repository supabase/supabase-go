package core

// ModulePath represents a distinct module within the Supabase Go SDK, where that
// module is capable of emitting telemetry - that is, where that module can have its
// interactions with the service observed by the service.
type ModulePath string

const basePath = "github.com/supabase/supabase-go/"

const (
	// ModulePathRoot is the import path for the Supabase Go SDK's convenience
	// entry point module, composed of other domain modules.
	ModulePathRoot ModulePath = basePath + "supabase"

	// ModulePathPostgrest is the import path for the Supabase Go SDK's PostgREST
	// client module.
	ModulePathPostgrest ModulePath = basePath + "postgrest"
)
