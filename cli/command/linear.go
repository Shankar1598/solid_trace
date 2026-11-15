package command

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The Linear app's install.
const (
	// LinearAuthorizeURL is Linear's approval page.
	LinearAuthorizeURL = "https://linear.app/oauth/authorize"
	// LinearCallbackPort is the default loopback port of the app's
	// callback URL, http://localhost:<port>/callback.
	LinearCallbackPort = 47615
	// linearScope lets the app read the tickets, set their status and
	// comment on them.
	linearScope = "read,write"
	// linearWait is how long the CLI waits for the approval.
	linearWait = 10 * time.Minute
)

// addLinear is 'connections add linear NAME --client-id ID', with the
// app's client secret on stdin. It opens Linear's approval page with
// actor=app, so that what the factory does shows as the app, gets the
// code at its loopback callback URL, and gives the code to the
// Dispatcher, which exchanges it for the tokens. The tokens never reach
// this machine.
func addLinear(ctx context.Context, args []string, env Env) int {
	const use = "solidtrace connections add linear NAME --client-id ID [--port PORT] < CLIENT_SECRET"
	flags := flag.NewFlagSet("connections add", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	dispatcherURL := flags.String("dispatcher", "", "the Dispatcher's URL (kept for the next commands)")
	clientID := flags.String("client-id", "", "the client ID of the Linear OAuth app")
	port := flags.Int("port", LinearCallbackPort, "the loopback port of the app's callback URL")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) != 2 || positional[0] != dispatcherapi.TrackerLinear || *clientID == "" || *port < 1 || *port > 65535 {
		fmt.Fprintln(env.Stderr, "solidtrace connections add: give "+use)
		return 2
	}
	name := positional[1]
	secret, err := readToken(env.Stdin)
	if err != nil {
		fmt.Fprintln(env.Stderr, "solidtrace connections add: give the client secret of the Linear app on stdin, such as: "+use)
		return 2
	}
	client, err := env.dispatcher(*dispatcherURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections add: %v\n", err)
		return 1
	}
	redirectURI := "http://localhost:" + strconv.Itoa(*port) + "/callback"
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(*port))
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections add: listen on the port %d: %v\n"+
			"Give another port with --port, and add its callback URL to the Linear app.\n", *port, err)
		return 1
	}
	defer listener.Close()

	state, verifier := randomText(), randomText()
	sum := sha256.Sum256([]byte(verifier))
	page, err := url.Parse(cmp.Or(env.LinearAuthorizeURL, LinearAuthorizeURL))
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections add: %v\n", err)
		return 1
	}
	page.RawQuery = url.Values{"client_id": {*clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"},
		"scope": {linearScope}, "actor": {"app"}, "prompt": {"consent"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}.Encode()

	codes := make(chan approval, 1)
	server := &http.Server{Handler: linearCallback(state, codes), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	fmt.Fprintf(env.Stdout, "The Linear app's callback URL must be %s.\n"+
		"Opening Linear's approval page. If it does not open, open this URL:\n%s\n", redirectURI, page)
	if env.OpenBrowser != nil {
		if err := env.OpenBrowser(page.String()); err != nil {
			fmt.Fprintf(env.Stderr, "Could not open a browser (%v): open the URL above.\n", err)
		}
	}
	wait, cancel := context.WithTimeout(ctx, linearWait)
	defer cancel()
	var got approval
	select {
	case got = <-codes:
	case <-wait.Done():
		fmt.Fprintf(env.Stderr, "solidtrace connections add: no approval in %s: run the command again\n", linearWait)
		return 1
	}
	if got.err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections add: %v\n", got.err)
		return 1
	}
	c, err := client.InstallLinear(ctx, name, dispatcherapi.InstallLinear{ClientID: *clientID, ClientSecret: secret,
		Code: got.code, RedirectURI: redirectURI, CodeVerifier: verifier})
	if err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace connections add: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "The linear connection %s is saved. The Dispatcher keeps its tokens encrypted and refreshes them.\n", c.Name)
	if c.WebhookURL != "" {
		fmt.Fprintf(env.Stdout, "Paste its webhook URL into the Linear app's webhook, with the Issue and Comment events: %s\n"+
			"Then give the app's signing secret: solidtrace connections webhook-secret %s < secret-file\n", c.WebhookURL, c.Name)
	} else {
		fmt.Fprintln(env.Stdout, "It has no webhook URL: the Dispatcher's configuration has no public webhook URL. "+
			"The backstop reads its tickets every 5 minutes.")
	}
	return 0
}

func randomText() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// approval is the code of an approval, or why there is none.
type approval struct {
	code string
	err  error
}

// linearCallback answers the first redirect with the right state, and
// sends its result. A redirect with a wrong state is refused and the wait
// goes on, so another program on the machine cannot end the install.
func linearCallback(state string, results chan<- approval) http.Handler {
	var mu sync.Mutex
	done := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		if done || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "This is not the answer to the install that solidtrace started.", http.StatusBadRequest)
			return
		}
		done = true
		result := approval{code: q.Get("code")}
		switch {
		case q.Get("error") == "access_denied":
			result = approval{err: errors.New("the install was denied on Linear's approval page")}
		case q.Get("error") != "":
			result = approval{err: fmt.Errorf("Linear's approval page answered %s %s", q.Get("error"), q.Get("error_description"))}
		case result.code == "":
			result = approval{err: errors.New("Linear's approval sent no code")}
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if result.err != nil {
			fmt.Fprintln(w, "The install failed. Go back to the terminal.")
		} else {
			fmt.Fprintln(w, "Linear approved the install. Go back to the terminal.")
		}
		results <- result
	})
	return mux
}
