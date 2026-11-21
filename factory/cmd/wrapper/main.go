// Command wrapper is the program that each Execution runs on the agent
// machine. The Runner starts it with the configuration that the wrapper
// package names (WRAPPER_*). With the argument stop-hook, it is the
// harness's Stop hook (wrapper.StopHook). With harness-env, it starts the
// harness of a take-over in its hc terminal (wrapper.HarnessEnv). With the
// argument version, it prints its version (the release that
// factory/image/build gives it).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/solidtrace/cli/version"
	"github.com/solidtrace/factory/wrapper"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(version.Version)
		return
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case wrapper.StopHookCommand:
			os.Exit(wrapper.StopHook(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
		case wrapper.HarnessEnvCommand:
			os.Exit(wrapper.HarnessEnv(os.Args[2:], os.Environ(), os.Stderr))
		}
	}
	os.Exit(wrapper.Main(context.Background(), os.Getenv, os.Environ(), os.Stderr))
}
