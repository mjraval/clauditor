package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testIdP is a fake Cloudflare Access: an RSA keypair + JWKS endpoint + signer.
type testIdP struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
	team   string // issuer domain (host of the JWKS server)
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &testIdP{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn-cgi/access/certs", func(w http.ResponseWriter, _ *http.Request) {
		pub := key.Public().(*rsa.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kid": idp.kid,
				"kty": "RSA",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	idp.team = strings.TrimPrefix(idp.server.URL, "http://")
	return idp
}

func (idp *testIdP) jwks() *JWKS {
	j := NewJWKS(idp.team)
	j.URL = idp.server.URL + "/cdn-cgi/access/certs" // httptest is http, not https
	return j
}

func (idp *testIdP) sign(t *testing.T, claims jwt.MapClaims, kid string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(idp.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (idp *testIdP) validClaims(aud string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":   "https://" + idp.team,
		"aud":   aud,
		"email": "user@example.com",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
}

func authFor(idp *testIdP, aud string) *AuthConfig {
	return &AuthConfig{TeamDomain: idp.team, PolicyAUD: aud, JWKS: idp.jwks()}
}

func doAuth(auth *AuthConfig, token string) *httptest.ResponseRecorder {
	var gotEmail string
	h := auth.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = EmailFrom(r.Context())
		fmt.Fprint(w, "ok:", gotEmail)
	}))
	req := httptest.NewRequest("GET", "/api/v1/state", nil)
	if token != "" {
		req.Header.Set("Cf-Access-Jwt-Assertion", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuth_ValidToken(t *testing.T) {
	idp := newTestIdP(t)
	auth := authFor(idp, "aud-123")
	rec := doAuth(auth, idp.sign(t, idp.validClaims("aud-123"), idp.kid))
	if rec.Code != 200 {
		t.Fatalf("valid token rejected: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "user@example.com") {
		t.Errorf("email claim not propagated: %s", rec.Body)
	}
}

func TestAuth_Rejections(t *testing.T) {
	idp := newTestIdP(t)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	tests := []struct {
		name  string
		token func() string
	}{
		{"missing token", func() string { return "" }},
		{"garbage", func() string { return "not.a.jwt" }},
		{"expired", func() string {
			c := idp.validClaims("aud-123")
			c["exp"] = time.Now().Add(-time.Hour).Unix()
			return idp.sign(t, c, idp.kid)
		}},
		{"wrong aud", func() string {
			return idp.sign(t, idp.validClaims("other-aud"), idp.kid)
		}},
		{"wrong issuer", func() string {
			c := idp.validClaims("aud-123")
			c["iss"] = "https://evil.example.com"
			return idp.sign(t, c, idp.kid)
		}},
		{"unknown kid", func() string {
			return idp.sign(t, idp.validClaims("aud-123"), "bogus-kid")
		}},
		{"forged signature", func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, idp.validClaims("aud-123"))
			tok.Header["kid"] = idp.kid
			s, _ := tok.SignedString(otherKey)
			return s
		}},
		{"alg none", func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodNone, idp.validClaims("aud-123"))
			tok.Header["kid"] = idp.kid
			s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
			return s
		}},
	}
	auth := authFor(idp, "aud-123")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAuth(auth, tt.token())
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("got %d, want 401; body=%s", rec.Code, rec.Body)
			}
		})
	}
}

func TestAuth_UnconfiguredRefuses(t *testing.T) {
	auth := &AuthConfig{}
	rec := doAuth(auth, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("unconfigured auth must refuse, got %d", rec.Code)
	}
}

func TestAuth_DevBypassLoopbackOnly(t *testing.T) {
	auth := &AuthConfig{DevInsecureLocal: true}
	h := auth.Auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("loopback dev bypass should pass, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.9:5555"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Error("dev bypass must NOT apply to non-loopback peers")
	}
}

func TestMutatingGuard(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "did it") }

	t.Run("actions disabled returns 403 with clear message", func(t *testing.T) {
		g := NewMutatingGuard(false, 10)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/dispatch", nil)
		req.Header.Set("X-Clauditor-Action", "1")
		g.Wrap("dispatch", ok).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "actions.enabled") {
			t.Errorf("got %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("missing X-Clauditor-Action rejected", func(t *testing.T) {
		g := NewMutatingGuard(true, 10)
		rec := httptest.NewRecorder()
		g.Wrap("dispatch", ok).ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("got %d", rec.Code)
		}
	})

	t.Run("wrong content type rejected", func(t *testing.T) {
		g := NewMutatingGuard(true, 10)
		req := httptest.NewRequest("POST", "/x", strings.NewReader("a=b"))
		req.Header.Set("X-Clauditor-Action", "1")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		g.Wrap("dispatch", ok).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("got %d", rec.Code)
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		g := NewMutatingGuard(true, 2)
		req := func() *http.Request {
			r := httptest.NewRequest("POST", "/x", nil)
			r.Header.Set("X-Clauditor-Action", "1")
			return r
		}
		h := g.Wrap("stop", ok)
		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req())
			if rec.Code != 200 {
				t.Fatalf("call %d: %d", i, rec.Code)
			}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req())
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("third call should be limited, got %d", rec.Code)
		}
	})
}
