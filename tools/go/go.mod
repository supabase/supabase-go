module github.com/supabase/supabase-go/tools/go

// The go line in this directory's go.work must be at least this version.
go 1.26.0

tool (
	github.com/kisielk/errcheck
	github.com/mgechev/revive
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
	mvdan.cc/gofumpt
)

require (
	github.com/google/uuid v1.6.0
	golang.org/x/net v0.59.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	codeberg.org/chavacava/garif v0.2.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/kisielk/errcheck v1.20.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mgechev/dots v1.0.0 // indirect
	github.com/mgechev/revive v1.17.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260908163034-4bcc4b2ee518 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	golang.org/x/vuln v1.8.0 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	honnef.co/go/tools v0.8.1 // indirect
	mvdan.cc/gofumpt v0.12.0 // indirect
)
