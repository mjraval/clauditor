package doctor

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// CheckAccessJWKS confirms Cloudflare Access's JWKS endpoint for teamDomain
// is fetchable and looks like a JWKS document (a "keys" array, even empty).
// It takes a plain *http.Client (not api.JWKS's caching type) so tests can
// point it at an httptest.Server instead of the network (SPEC §16: no test
// touches the network). SKIPs when teamDomain is unconfigured.
func CheckAccessJWKS(teamDomain string, client *http.Client) Check {
	if teamDomain == "" {
		return Check{"access JWKS", SKIP, "access.team_domain not configured"}
	}
	return checkAccessJWKSAt("https://"+teamDomain+"/cdn-cgi/access/certs", client)
}

// checkAccessJWKSAt does the actual fetch-and-validate against an explicit
// URL, split out so tests can point it at an httptest.Server (which can't
// mint an https://<team_domain> URL) without touching the real network.
func checkAccessJWKSAt(url string, client *http.Client) Check {
	const name = "access JWKS"
	resp, err := client.Get(url)
	if err != nil {
		return Check{name, FAIL, fmt.Sprintf("GET %s: %v", url, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Check{name, FAIL, fmt.Sprintf("GET %s: status %d", url, resp.StatusCode)}
	}
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return Check{name, FAIL, fmt.Sprintf("decode JWKS from %s: %v", url, err)}
	}
	if doc.Keys == nil {
		return Check{name, FAIL, fmt.Sprintf("%s: response has no \"keys\" array", url)}
	}
	return Check{name, PASS, fmt.Sprintf("%d keys", len(doc.Keys))}
}
