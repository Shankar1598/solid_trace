// Command dispatcher is the Dispatcher: one binary with SQLite, on the
// server VM beside the Nomad server. It reads the operator configuration
// at start, from the file that -config (or DISPATCHER_CONFIG) names and
// from the environment, and serves the private listener.
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
	"github.com/solidtrace/factory/nomadrunner"
)

// Version is the release that this binary is, such as v0.19.0. A release
// build sets it with -ldflags "-X main.Version=v0.19.0". A build from a
// checkout is "dev", and accepts each solidtrace CLI.
var Version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stderr))
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
	options := dispatcher.Options{Log: log, Version: Version}
	// With no Nomad address there is no Runner: each Run stays queued.
	if config.NomadAddress != "" {
		runner, err := nomadrunner.New(nomadrunner.FromConfig(config, log))
		if err != nil {
			return fail(err)
		}
		defer runner.Close()
		options.Runner = runner
	}
	d, err := dispatcher.Open(config, options)
	if err != nil {
		return fail(err)
	}
	defer d.Close()

	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return fail(fmt.Errorf("listen on %s (%s): %w", config.Listen, dispatcher.EnvListen, err))
	}
	servers := []*http.Server{{Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second}}
	listeners := []net.Listener{listener}
	if config.WebhookListen != "" {
		webhooks, err := net.Listen("tcp", config.WebhookListen)
		if err != nil {
			listener.Close()
			return fail(fmt.Errorf("listen on %s (%s): %w", config.WebhookListen, dispatcher.EnvWebhookListen, err))
		}
		// The webhook listener is the Dispatcher's one public path: short
		// timeouts, so that slow senders cannot hold its connections.
		servers = append(servers, &http.Server{Handler: d.WebhookHandler(), ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute,
			MaxHeaderBytes: 64 << 10})
		listeners = append(listeners, webhooks)
	}
	served := make(chan error, len(servers))
	for i, server := range servers {
		go func() { served <- server.Serve(listeners[i]) }()
	}
	log.Info("the Dispatcher serves the private listener", "address", listener.Addr().String())
	if len(listeners) > 1 {
		log.Info("the Dispatcher serves the webhook listener", "address", listeners[1].Addr().String(),
			"public_url", config.WebhookURL)
	}

	var failed error
	select {
	case failed = <-served:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) && failed == nil {
			failed = err
		}
	}
	if failed != nil {
		return fail(failed)
	}
	return 0
}
