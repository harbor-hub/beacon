package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// apiHandler serves /api/*. When APIToken is set, every request must send
// "Authorization: Bearer <token>".
func (b *Beacon) apiHandler(registry *Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/records", recordsHandler(registry, b.RecordLabels))

	return requireToken(b.APIToken, mux)
}

// apiWarnings reports API settings that have no effect or are unsafe.
func (b *Beacon) apiWarnings() []string {
	var warnings []string

	if b.APIAddr == "" {
		if b.APIToken != "" {
			warnings = append(warnings, "--api-token has no effect without --api-listen")
		}
		if b.RecordLabels != "" {
			warnings = append(warnings, "--record-labels has no effect without --api-listen")
		}
		return warnings
	}

	if b.APIToken == "" && !isLoopbackAddr(b.APIAddr) {
		warnings = append(warnings, fmt.Sprintf("API on %s is not loopback-only and has no --api-token; do not publish this port", b.APIAddr))
	}

	return warnings
}

// requireToken rejects requests without "Authorization: Bearer <token>".
// An empty token disables the check.
func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}

	expected := []byte("Bearer " + token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recordsHandler serves GET /api/records. Only labels whose key starts with
// recordLabels are included (none when it is empty).
func recordsHandler(registry *Registry, recordLabels string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		records := registry.List()
		for i := range records {
			records[i].Labels = filterLabels(records[i].Labels, recordLabels)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Records []Record `json:"records"`
		}{records})
	}
}

// filterLabels returns a new map with the labels whose key starts with prefix.
func filterLabels(labels map[string]string, prefix string) map[string]string {
	if prefix == "" {
		return nil
	}

	var out map[string]string
	for k, v := range labels {
		if strings.HasPrefix(k, prefix) {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}

	return out
}
