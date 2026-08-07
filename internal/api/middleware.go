package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey int

const emailKey ctxKey = 0

// AuthConfig configures the Access-JWT middleware (SPEC §9).
type AuthConfig struct {
	TeamDomain string // e.g. "team.cloudflareaccess.com"
	PolicyAUD  string
	JWKS       *JWKS
	// DevInsecureLocal disables auth for loopback peers only
	// (--dev-insecure-local). A loud warning is logged every 60s while used.
	DevInsecureLocal bool

	warnLast time.Time
	warnMu   sync.Mutex
}

// Auth validates the Cloudflare Access JWT on every request it wraps.
func (ac *AuthConfig) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ac.DevInsecureLocal && isLoopback(r.RemoteAddr) {
			ac.warnMu.Lock()
			if time.Since(ac.warnLast) > 60*time.Second {
				ac.warnLast = time.Now()
				slog.Warn("AUTH DISABLED: --dev-insecure-local is active — loopback requests bypass Access JWT validation")
			}
			ac.warnMu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		if ac.TeamDomain == "" || ac.PolicyAUD == "" {
			writeErr(w, http.StatusForbidden, "auth_unconfigured",
				"access.team_domain and access.policy_aud must be set (or run with --dev-insecure-local on loopback)")
			return
		}

		raw := r.Header.Get("Cf-Access-Jwt-Assertion")
		if raw == "" {
			if c, err := r.Cookie("CF_Authorization"); err == nil {
				raw = c.Value
			}
		}
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "missing_token", "no Access JWT presented")
			return
		}

		claims := jwt.MapClaims{}
		tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			return ac.JWKS.Key(kid)
		},
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithIssuer("https://"+ac.TeamDomain),
			jwt.WithAudience(ac.PolicyAUD),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(30*time.Second),
		)
		if err != nil || !tok.Valid {
			writeErr(w, http.StatusUnauthorized, "invalid_token", "Access JWT validation failed")
			return
		}
		email, _ := claims["email"].(string)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), emailKey, email)))
	})
}

// EmailFrom returns the authenticated email claim, if any.
func EmailFrom(ctx context.Context) string {
	e, _ := ctx.Value(emailKey).(string)
	return e
}

// MutatingGuard enforces the extra gates on state-changing routes:
// actions.enabled, X-Clauditor-Action header (CSRF blunting), JSON
// content-type, and a per-route rate limit. It also logs the authenticated
// email at info (never the body — prompts may hold secrets).
type MutatingGuard struct {
	ActionsEnabled bool
	limiter        *rateLimiter
}

func NewMutatingGuard(enabled bool, perMinute int) *MutatingGuard {
	return &MutatingGuard{ActionsEnabled: enabled, limiter: newRateLimiter(perMinute)}
}

func (g *MutatingGuard) Wrap(name string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.ActionsEnabled {
			writeErr(w, http.StatusForbidden, "actions_disabled",
				"mutating actions are disabled — set actions.enabled = true in config to allow them")
			return
		}
		if r.Header.Get("X-Clauditor-Action") != "1" {
			writeErr(w, http.StatusForbidden, "missing_action_header",
				"mutating requests require header X-Clauditor-Action: 1")
			return
		}
		if r.Method == http.MethodPost && r.ContentLength != 0 {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "bad_content_type",
					"mutating requests require Content-Type: application/json")
				return
			}
		}
		if !g.limiter.allow(name, time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited",
				"per-route rate limit exceeded")
			return
		}
		slog.Info("action", "route", name, "email", EmailFrom(r.Context()))
		next(w, r)
	})
}

// rateLimiter is a minimal fixed-window per-key counter.
type rateLimiter struct {
	mu        sync.Mutex
	perMinute int
	window    map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(perMinute int) *rateLimiter {
	if perMinute <= 0 {
		perMinute = 10
	}
	return &rateLimiter{perMinute: perMinute, window: map[string]*rateWindow{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.window[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		l.window[key] = &rateWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.perMinute
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
