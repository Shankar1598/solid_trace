package dispatcher

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testJWKS is a Runner's key set that counts its reads. While block is
// not nil, each read waits until it is closed.
type testJWKS struct {
	key   *rsa.PrivateKey
	kid   atomic.Value
	reads atomic.Int32
	asked chan struct{}
	block atomic.Value
}

func newTestJWKS(t *testing.T) (*testJWKS, *httptest.Server) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	j := &testJWKS{key: key, asked: make(chan struct{}, 100)}
	j.kid.Store("k1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		j.reads.Add(1)
		select {
		case j.asked <- struct{}{}:
		default:
		}
		if block, _ := j.block.Load().(chan struct{}); block != nil {
			<-block
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{
			"kty": "RSA", "kid": j.kid.Load().(string),
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	return j, srv
}

func (j *testJWKS) sign(t *testing.T, kid string, now time.Time) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid})
	body, _ := json.Marshal(map[string]any{"sub": "w", "aud": IdentityAudience, "exp": now.Add(time.Hour).Unix()})
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, j.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestJWTsWithAnUnknownKeyReadTheJWKSAtMostOnceInAnInterval(t *testing.T) {
	jwks, srv := newTestJWKS(t)
	clock := &testClock{now: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	check := &identityCheck{jwksURL: srv.URL, http: &http.Client{Timeout: 10 * time.Second}, now: clock.Now}
	ctx := context.Background()
	if _, err := check.workload(ctx, jwks.sign(t, "k1", clock.Now())); err != nil {
		t.Fatal(err)
	}

	many := func() {
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := check.workload(ctx, jwks.sign(t, "r"+strconv.Itoa(i), clock.Now()))
				if !errors.Is(err, errBadIdentity) {
					t.Errorf("a JWT with an unknown key: %v", err)
				}
			}()
		}
		wg.Wait()
	}
	many()
	if got := jwks.reads.Load(); got != 1 {
		t.Errorf("the JWKS was read %d times in the interval, want 1", got)
	}
	clock.Advance(jwksInterval)
	many()
	if got := jwks.reads.Load(); got != 2 {
		t.Errorf("the JWKS was read %d times after the interval, want 2", got)
	}

	// The Runner turned its keys: a JWT with the new key is valid after
	// the interval.
	jwks.kid.Store("k2")
	clock.Advance(jwksInterval)
	if _, err := check.workload(ctx, jwks.sign(t, "k2", clock.Now())); err != nil {
		t.Errorf("a JWT with the Runner's new key: %v", err)
	}
}

func TestAJWTWithAKnownKeyDoesNotWaitForAReadOfTheJWKS(t *testing.T) {
	jwks, srv := newTestJWKS(t)
	clock := &testClock{now: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	check := &identityCheck{jwksURL: srv.URL, http: &http.Client{Timeout: 10 * time.Second}, now: clock.Now}
	ctx := context.Background()
	if _, err := check.workload(ctx, jwks.sign(t, "k1", clock.Now())); err != nil {
		t.Fatal(err)
	}
	<-jwks.asked

	clock.Advance(jwksInterval)
	block := make(chan struct{})
	jwks.block.Store(block)
	slow := make(chan error, 1)
	go func() {
		_, err := check.workload(ctx, jwks.sign(t, "unknown", clock.Now()))
		slow <- err
	}()
	<-jwks.asked

	done := make(chan error, 1)
	go func() {
		_, err := check.workload(ctx, jwks.sign(t, "k1", clock.Now()))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a JWT with a known key: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("a JWT with a known key waited for the read of the JWKS")
	}
	close(block)
	if err := <-slow; !errors.Is(err, errBadIdentity) {
		t.Errorf("a JWT with an unknown key: %v", err)
	}
}
