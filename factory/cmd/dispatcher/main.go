// Command dispatcher is the Dispatcher: one binary with SQLite, on the
// server VM beside the Nomad server. It reads the operator configuration
// at start, from the file that -config (or DISPATCHER_CONFIG) names and
// from the environment, and serves the private listener. 'dispatcher
// accounts' adds, lists and removes the subscription accounts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/solidtrace/factory/dispatcher"
)

// Version is the release that this binary is, such as v0.19.0. A release
// build sets it with -ldflags "-X main.Version=v0.19.0". A build from a
// checkout is "dev", and accepts each solidtrace CLI.
var Version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(command(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}

// command is 'dispatcher accounts ...', the operator command for the pool
// of subscription accounts, or else the Dispatcher itself (run).
func command(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader,
	stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "accounts" {
		return dispatcher.AccountsCommand(args[1:], getenv, time.Now, stdin, stdout, stderr)
	}
	return run(ctx, args, getenv, stderr)
}

// run starts the Dispatcher and serves until ctx ends. It returns the exit
// code: 1 when it cannot start, 2 for a wrong command line.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	flags := flag.NewFlagSet("dispatcher", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configFile := flags.String("config", getenv(dispatcher.EnvConfigFile),
		"the operator configuration file (NAME=value lines); the environment wins over it")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	fail := func(err error) int {
		fmt.Fprintf(stderr, "dispatcher: %v\n", err)
		return 1
	}

	config, err := dispatcher.LoadConfig(*configFile, getenv)
	if err != nil {
		return fail(err)
	}
	d, err := dispatcher.Open(config, dispatcher.Options{Log: log, Version: Version})
	if err != nil {
		return fail(err)
	}
	defer d.Close()

	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return fail(fmt.Errorf("listen on %s (%s): %w", config.Listen, dispatcher.EnvListen, err))
	}
	server := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Info("the Dispatcher serves the private listener", "address", listener.Addr().String())

	select {
	case err := <-served:
		return fail(err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(err)
	}
	return 0
}
