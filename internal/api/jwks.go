package api

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWKS fetches and caches Cloudflare Access's signing keys from
// https://<team_domain>/cdn-cgi/access/certs, refreshing on unknown kid
// (key rotation) with a minimum interval between fetches.
type JWKS struct {
	URL    string
	Client *http.Client

	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	lastFetch  time.Time
	minRefetch time.Duration
}

// NewJWKS builds a JWKS cache for a team domain (e.g. "team.cloudflareaccess.com").
func NewJWKS(teamDomain string) *JWKS {
	return &JWKS{
		URL:        "https://" + teamDomain + "/cdn-cgi/access/certs",
		Client:     &http.Client{Timeout: 10 * time.Second},
		keys:       map[string]*rsa.PublicKey{},
		minRefetch: 30 * time.Second,
	}
}

// Key returns the RSA public key for kid, refetching once when unknown.
func (j *JWKS) Key(kid string) (*rsa.PublicKey, error) {
	j.mu.RLock()
	k, ok := j.keys[kid]
	stale := time.Since(j.lastFetch) > time.Hour
	j.mu.RUnlock()
	if ok && !stale {
		return k, nil
	}
	if err := j.refresh(); err != nil {
		if ok {
			return k, nil // rotation-tolerant: serve the cached key on fetch failure
		}
		return nil, err
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if k, ok := j.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

func (j *JWKS) refresh() error {
	j.mu.Lock()
	if time.Since(j.lastFetch) < j.minRefetch {
		j.mu.Unlock()
		return nil
	}
	j.lastFetch = time.Now()
	j.mu.Unlock()

	resp, err := j.Client.Get(j.URL)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("jwks decode: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := parseRSAKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("jwks: no usable RSA keys")
	}
	j.mu.Lock()
	j.keys = keys
	j.mu.Unlock()
	return nil
}

func parseRSAKey(n64, e64 string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(n64)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(e64)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eb {
		e = e<<8 | int(b)
	}
	if e <= 0 {
		return nil, fmt.Errorf("bad exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
}
