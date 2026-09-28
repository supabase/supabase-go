module github.com/supabase/supabase-go/tools/go

// The go line in this directory's go.work must be at least this version.
go 1.26.0

tool (
	github.com/kisielk/errcheck
	github.com/mgechev/revive
	golang.org/x/tools/gopls
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
	mvdan.cc/gofumpt
)

require (
	github.com/google/uuid v1.6.0
	golang.org/x/net v0.56.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	codeberg.org/chavacava/garif v0.2.0 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/fatih/gomodifytags v1.17.1-0.20250423142747-f3939df9aa3c // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/hashicorp/go-version v1.8.0 // indirect
	github.com/kisielk/errcheck v1.20.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mgechev/dots v1.0.0 // indirect
	github.com/mgechev/revive v1.15.0 // indirect
	github.com/modelcontextprotocol/go-sdk v1.6.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/telemetry v0.0.0-20260625142307-59b4966ccb57 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.47.1-0.20260707181000-a299dadba899 // indirect
	golang.org/x/tools/gopls v0.23.0 // indirect
	golang.org/x/vuln v1.5.0 // indirect
	honnef.co/go/tools v0.8.0-rc.1 // indirect
	mvdan.cc/gofumpt v0.10.0 // indirect
	mvdan.cc/xurls/v2 v2.6.0 // indirect
)
