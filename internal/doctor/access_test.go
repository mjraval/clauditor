package doctor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCheckAccessJWKS never touches the network (SPEC §16): every non-SKIP
// case points the client at an httptest.Server via client.Transport, since
// CheckAccessJWKS hardcodes an https:// URL that httptest.NewServer can't
// produce directly.
func TestCheckAccessJWKS(t *testing.T) {
	t.Run("empty team domain skips", func(t *testing.T) {
		got := CheckAccessJWKS("", &http.Client{})
		if got.Status != SKIP {
			t.Fatalf("status = %s, want SKIP", got.Status)
		}
	})

	t.Run("200 with keys array is PASS", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"keys":[{"kid":"a"}]}`))
		}))
		defer srv.Close()
		got := checkAccessJWKSAt(srv.URL, srv.Client())
		if got.Status != PASS {
			t.Fatalf("status = %s, want PASS (detail: %s)", got.Status, got.Detail)
		}
	})

	t.Run("200 with empty keys array is still PASS", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"keys":[]}`))
		}))
		defer srv.Close()
		got := checkAccessJWKSAt(srv.URL, srv.Client())
		if got.Status != PASS {
			t.Fatalf("status = %s, want PASS (detail: %s)", got.Status, got.Detail)
		}
	})

	t.Run("200 with no keys field is FAIL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"notkeys":true}`))
		}))
		defer srv.Close()
		got := checkAccessJWKSAt(srv.URL, srv.Client())
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})

	t.Run("non-200 is FAIL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		got := checkAccessJWKSAt(srv.URL, srv.Client())
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL", got.Status)
		}
	})

	t.Run("unreachable host is FAIL, offline", func(t *testing.T) {
		// A domain that resolves nowhere useful and a client with a hard
		// timeout: this still never touches the real network meaningfully
		// (localhost port nothing is listening on), matching SPEC §16.
		got := checkAccessJWKSAt("http://127.0.0.1:1", &http.Client{})
		if got.Status != FAIL {
			t.Fatalf("status = %s, want FAIL (detail: %s)", got.Status, got.Detail)
		}
		if !strings.Contains(got.Detail, "127.0.0.1:1") {
			t.Errorf("detail should name the URL: %s", got.Detail)
		}
	})
}
