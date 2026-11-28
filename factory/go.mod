module github.com/solidtrace/factory

go 1.27.4

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/hashicorp/cronexpr v1.1.3
	github.com/hashicorp/hcl/v2 v2.25.0
	github.com/hashicorp/nomad/api v0.0.0-20261029144823-d0f210429976
	github.com/mattn/go-sqlite3 v1.14.53
	github.com/solidtrace/cli v0.0.0
	github.com/zclconf/go-cty v1.19.0
)

require (
	github.com/agext/levenshtein v1.2.1 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/apparentlymart/go-textseg/v17 v17.0.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/mod v0.29.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// The factory module may import the CLI's packages, because the CLI is a
// client of public APIs. It imports nothing from Console or EventStore
// (ADR 0019); imports_test.go checks this.
replace github.com/solidtrace/cli => ../cli
