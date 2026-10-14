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
	golang.org/x/net v0.61.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	codeberg.org/chavacava/garif v0.2.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/kisielk/errcheck v1.30.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mgechev/revive v1.17.1 // indirect
	github.com/revive-lint/dots v1.1.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	honnef.co/go/tools v0.8.2 // indirect
	mvdan.cc/gofumpt v0.12.0 // indirect
)
