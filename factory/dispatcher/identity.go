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
// a JWT names a key that it does not have (the Runner turned its keys).
type identityCheck struct {
	jwksURL string
	http    *http.Client
	now     func() time.Time

	mu   sync.Mutex
	keys map[string]*rsa.PublicKey
}

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

// workload checks token and gives its subject: the workload that the
// Runner said that it started for an Execution. Each failure is
// errBadIdentity (with the reason, for the log).
func (c *identityCheck) workload(ctx context.Context, token string) (string, error) {
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

// key gives the key with id kid, and reads the JWKS again if it does not
// have it.
func (c *identityCheck) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	keys, err := c.fetch(ctx)
	if err != nil {
		return nil, err
	}
	c.keys = keys
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: the Runner's JWKS has no key %q", errBadIdentity, kid)
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
