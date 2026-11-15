package dispatcher

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IdentityAudience is the audience that a wrapper's workload identity JWT
// must have. The Nomad job template asks for it in its identity block.
const IdentityAudience = "solidtrace-dispatcher"

// identitySkew is the clock difference between the Runner and the
// Dispatcher that the check accepts for nbf and iat.
const identitySkew = time.Minute

var errBadIdentity = errors.New("the workload identity is not valid")

// identityCheck checks a wrapper's workload identity JWT against the
// Runner's JWKS (RS256). It keeps the keys, and reads the JWKS again when
// a JWT names a key that it does not have (the Runner turned its keys),
// at most once in jwksInterval.
type identityCheck struct {
	jwksURL string
	http    *http.Client
	now     func() time.Time

	mu   sync.Mutex
	keys map[string]*rsa.PublicKey
	// read is the time of the last read of the JWKS; reading is the read
	// that is in progress (nil when there is none).
	read    time.Time
	reading *jwksRead
}

// jwksRead is one read of the JWKS. done closes when it ends.
type jwksRead struct {
	done chan struct{}
	err  error
}

// jwksInterval is the shortest time between two reads of the JWKS. Any
// caller can send a JWT with a kid that is not known: without this limit,
// each such JWT would read the JWKS again.
const jwksInterval = 30 * time.Second

type identityClaims struct {
	Subject   string   `json:"sub"`
	Audience  audience `json:"aud"`
	Expiry    *int64   `json:"exp"`
	NotBefore *int64   `json:"nbf"`
	IssuedAt  *int64   `json:"iat"`
}

// audience is a JWT's aud: one string or a list.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience{one}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*a = list
	return nil
}

// Identity is a workload identity JWT that the Dispatcher checked: the
// Runner signed it for the Dispatcher's audience, and it is valid now.
// The Runner's Workload gives the workload that holds it.
type Identity struct {
	Subject string
	// Claims are each claim of the JWT, as JSON.
	Claims map[string]json.RawMessage
}

// StringClaim gives the claim name when it is a JSON string, else "".
func (i Identity) StringClaim(name string) string {
	var v string
	if json.Unmarshal(i.Claims[name], &v) != nil {
		return ""
	}
	return v
}

// check checks token and gives the identity that it holds. Each failure
// is errBadIdentity (with the reason, for the log).
func (c *identityCheck) check(ctx context.Context, token string) (Identity, error) {
	sub, err := c.verify(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	var claims map[string]json.RawMessage
	if err := decodeSegment(strings.Split(token, ".")[1], &claims); err != nil {
		return Identity{}, fmt.Errorf("%w: claims: %v", errBadIdentity, err)
	}
	return Identity{Subject: sub, Claims: claims}, nil
}

// verify checks the signature and the standard claims of token, and gives
// its subject.
func (c *identityCheck) verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: not a JWT", errBadIdentity)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return "", fmt.Errorf("%w: header: %v", errBadIdentity, err)
	}
	if header.Alg != "RS256" {
		return "", fmt.Errorf("%w: alg %q is not RS256", errBadIdentity, header.Alg)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("%w: signature: %v", errBadIdentity, err)
	}
	key, err := c.key(ctx, header.Kid)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return "", fmt.Errorf("%w: the signature does not match key %q", errBadIdentity, header.Kid)
	}
	var claims identityClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return "", fmt.Errorf("%w: claims: %v", errBadIdentity, err)
	}
	now := c.now()
	switch {
	case claims.Expiry == nil || !now.Before(time.Unix(*claims.Expiry, 0)):
		return "", fmt.Errorf("%w: expired or no exp", errBadIdentity)
	case claims.NotBefore != nil && now.Add(identitySkew).Before(time.Unix(*claims.NotBefore, 0)):
		return "", fmt.Errorf("%w: not valid yet", errBadIdentity)
	case claims.IssuedAt != nil && now.Add(identitySkew).Before(time.Unix(*claims.IssuedAt, 0)):
		return "", fmt.Errorf("%w: issued in the future", errBadIdentity)
	case !contains(claims.Audience, IdentityAudience):
		return "", fmt.Errorf("%w: the audience is not %s", errBadIdentity, IdentityAudience)
	case claims.Subject == "":
		return "", fmt.Errorf("%w: no subject", errBadIdentity)
	}
	return claims.Subject, nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func decodeSegment(segment string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// key gives the key with id kid. When it does not have that key, it reads
// the JWKS again, at most once in jwksInterval. Only one read is in
// progress at a time, and the read is not done under c.mu, so that a JWT
// with a known key never waits for it.
func (c *identityCheck) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	for checked := false; ; checked = true {
		c.mu.Lock()
		if key, ok := c.keys[kid]; ok {
			c.mu.Unlock()
			return key, nil
		}
		if read := c.reading; read != nil {
			c.mu.Unlock()
			select {
			case <-read.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if read.err != nil {
				return nil, read.err
			}
			continue
		}
		if checked || (!c.read.IsZero() && c.now().Sub(c.read) < jwksInterval) {
			c.mu.Unlock()
			return nil, fmt.Errorf("%w: the Runner's JWKS has no key %q", errBadIdentity, kid)
		}
		read := &jwksRead{done: make(chan struct{})}
		c.reading = read
		c.mu.Unlock()

		// The read is for all callers that wait on it: a caller that goes
		// away does not stop it.
		keys, err := c.fetch(context.WithoutCancel(ctx))
		c.mu.Lock()
		c.reading, read.err = nil, err
		if err == nil {
			c.keys, c.read = keys, c.now()
		}
		c.mu.Unlock()
		close(read.done)
		if err != nil {
			return nil, err
		}
	}
}

func (c *identityCheck) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read the Runner's JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read the Runner's JWKS: %s answered %d", c.jwksURL, resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("read the Runner's JWKS: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil || len(e) > 4 {
			continue
		}
		exponent := int(new(big.Int).SetBytes(e).Int64())
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	return keys, nil
}
