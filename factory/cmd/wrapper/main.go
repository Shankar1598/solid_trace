// Command wrapper is the program that each Execution runs on the agent
// machine. The Runner starts it with the configuration that the wrapper
// package names (WRAPPER_*). With the argument stop-hook, it is the
// harness's Stop hook (wrapper.StopHook).
package main

import (
	"context"
	"os"

	"github.com/solidtrace/factory/wrapper"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == wrapper.StopHookCommand {
		os.Exit(wrapper.StopHook(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
	}
	os.Exit(wrapper.Main(context.Background(), os.Getenv, os.Environ(), os.Stderr))
}
