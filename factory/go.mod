module github.com/solidtrace/factory

go 1.27.3

require (
	github.com/hashicorp/cronexpr v1.1.3
	github.com/mattn/go-sqlite3 v1.14.33
	github.com/solidtrace/cli v0.0.0
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/nomad/api v0.0.0-20261029144823-d0f210429976 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
)

// The factory module may import the CLI's packages, because the CLI is a
// client of public APIs. It imports nothing from Console or EventStore
// (ADR 0019); imports_test.go checks this.
replace github.com/solidtrace/cli => ../cli
