package signin

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ErrNoBrowser is the error of Browser when open cannot open the approval
// page. The caller can use the device flow then.
var ErrNoBrowser = errors.New("could not open a browser")

// Browser signs in with the browser grant: it listens on a loopback port
// at 127.0.0.1, opens the approval page with open, and waits for the
// redirect with the code. Then it exchanges the code (with the PKCE
// verifier) for the tokens. It prints the approval page's URL to out, for
// a person whose browser did not open.
//
// A redirect with a wrong state is refused and the wait goes on, so another
// program on the machine cannot end the sign-in. A redirect with the right
// state and an error, or with a wrong iss, ends it.
func (c *Client) Browser(ctx context.Context, server Server, open func(string) error, out io.Writer) (Tokens, error) {
	if server.AuthorizationEndpoint == "" {
		return Tokens{}, errors.New("the server has no approval page for the browser sign-in")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Tokens{}, fmt.Errorf("listen on a loopback port: %w", err)
	}
	defer listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	state := random(32)
	verifier := random(32)
	authorize, err := url.Parse(server.AuthorizationEndpoint)
	if err != nil {
		return Tokens{}, err
	}
	query := authorize.Query()
	for key, value := range map[string]string{
		"response_type":         "code",
		"client_id":             ClientID,
		"redirect_uri":          redirectURI,
		"scope":                 Scope,
		"resource":              server.Resource(),
		"state":                 state,
		"code_challenge":        s256(verifier),
		"code_challenge_method": "S256",
	} {
		query.Set(key, value)
	}
	authorize.RawQuery = query.Encode()

	results := make(chan callback, 1)
	httpServer := &http.Server{
		Handler:           callbackHandler(state, server, results),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()

	fmt.Fprintf(out, "Opening the approval page in your browser. If it does not open, open this URL:\n%s\n", authorize)
	if err := open(authorize.String()); err != nil {
		return Tokens{}, fmt.Errorf("%w: %v", ErrNoBrowser, err)
	}

	ctx, cancel := context.WithTimeout(ctx, BrowserWait)
	defer cancel()
	var result callback
	select {
	case result = <-results:
	case <-ctx.Done():
		return Tokens{}, fmt.Errorf("no approval in %s: run 'solidtrace login' again", BrowserWait)
	}
	if result.err != nil {
		return Tokens{}, result.err
	}
	return c.token(ctx, server.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {result.code},
		"redirect_uri":  {redirectURI},
		"client_id":     {ClientID},
		"code_verifier": {verifier},
		"resource":      {server.Resource()},
	})
}

type callback struct {
	code string
	err  error
}

func callbackHandler(state string, server Server, results chan<- callback) http.Handler {
	// One answer only: the first redirect with the right state.
	var mu sync.Mutex
	done := false
	finish := func(w http.ResponseWriter, result callback, page string) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, page)
		done = true
		results <- result
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 || done {
			http.Error(w, "This is not the answer to the sign-in that solidtrace started.", http.StatusBadRequest)
			return
		}
		if server.IssInResponse && q.Get("iss") != server.Issuer {
			finish(w, callback{err: fmt.Errorf("the approval came from another server (iss %q), not %s: the sign-in stopped",
				q.Get("iss"), server.Issuer)}, "The sign-in failed. Go back to the terminal.")
			return
		}
		if code := q.Get("error"); code != "" {
			err := &Error{Code: code, Description: q.Get("error_description")}
			if code == "access_denied" {
				err.Description = "you denied the sign-in"
			}
			finish(w, callback{err: err}, "The sign-in failed. Go back to the terminal.")
			return
		}
		if q.Get("code") == "" {
			finish(w, callback{err: errors.New("the approval sent no code")}, "The sign-in failed. Go back to the terminal.")
			return
		}
		finish(w, callback{code: q.Get("code")}, "You are signed in to SolidTrace. Go back to the terminal.")
	})
	return mux
}
