package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// httpHandler – mounting
// ---------------------------------------------------------------------------

func TestHTTPHandler_Routes(t *testing.T) {
	t.Parallel()

	b := &Beacon{}
	h := b.httpHandler(NewRegistry())

	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/records", http.StatusOK},
		{http.MethodPost, "/api/records", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/records/x", http.StatusNotFound},
		{http.MethodGet, "/api/other", http.StatusNotFound},
		{http.MethodGet, "/records", http.StatusNotFound},
		{http.MethodGet, "/", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
}

func TestHTTPHandler_TokenOnlyForAPI(t *testing.T) {
	t.Parallel()

	b := &Beacon{APIToken: "secret"}
	h := b.httpHandler(NewRegistry())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/records", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/api/records without token: status %d, want 401", w.Code)
	}

	// Paths outside /api/ are not affected by the token.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("/ : status %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// newHTTPServer
// ---------------------------------------------------------------------------

func TestNewHTTPServer_Timeouts(t *testing.T) {
	t.Parallel()

	s := newHTTPServer(":0", http.NotFoundHandler())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 {
		t.Errorf("all timeouts must be set: %+v", s)
	}
}

// ---------------------------------------------------------------------------
// isLoopbackAddr
// ---------------------------------------------------------------------------

func TestIsLoopbackAddr(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"127.0.0.1:8080": true,
		"[::1]:8080":     true,
		"localhost:8080": true,
		"0.0.0.0:8080":   false,
		":8080":          false,
		"10.0.0.1:8080":  false,
		"invalid":        false,
	}

	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
