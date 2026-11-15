// Command wrapper is the program that each Execution runs on the agent
// machine. The Runner starts it with the configuration that the wrapper
// package names (WRAPPER_*).
package main

import (
	"context"
	"os"

	"github.com/solidtrace/factory/wrapper"
)

func main() {
	os.Exit(wrapper.Main(context.Background(), os.Getenv, os.Environ(), os.Stderr))
}
