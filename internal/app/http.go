package app

import (
	"net"
	"net/http"
	"time"
)

// newHTTPServer returns the HTTP server for addr with timeouts, so a slow or
// idle client cannot hold a connection forever.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// httpHandler mounts Beacon's HTTP endpoints.
func (b *Beacon) httpHandler(registry *Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", b.apiHandler(registry))

	return mux
}

// isLoopbackAddr reports whether a listen address only accepts local
// connections.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
