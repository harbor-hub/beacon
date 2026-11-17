package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func getRecords(t *testing.T, h http.Handler, auth string) (int, []Record) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		return w.Code, nil
	}

	var body struct {
		Records []Record `json:"records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", w.Body, err)
	}

	return w.Code, body.Records
}

// ---------------------------------------------------------------------------
// GET /api/records
// ---------------------------------------------------------------------------

func TestRecords_Empty(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	w := httptest.NewRecorder()
	recordsHandler(NewRegistry(), "").ServeHTTP(w, req)

	if got := w.Body.String(); got != "{\"records\":[]}\n" {
		t.Errorf("body = %q, want empty records array", got)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestRecords_All(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.SetRecord(Record{Host: "Web.Beacon", Target: "172.17.0.2", Container: "web"})
	reg.Set("api.beacon", "172.17.0.3")

	code, records := getRecords(t, recordsHandler(reg, ""), "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}

	want := []Record{
		{Host: "api.beacon", Target: "172.17.0.3"},
		{Host: "web.beacon", Target: "172.17.0.2", Container: "web"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Errorf("records = %+v, want %+v", records, want)
	}
}

func TestRecords_LabelsFilteredByPrefix(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		"beacon.host":  "web.beacon",
		"proxy.port":   "3000",
		"proxy.scheme": "https",
		"other":        "x",
	}
	reg := NewRegistry()
	reg.SetRecord(Record{Host: "web.beacon", Target: "172.17.0.2", Labels: labels})

	_, records := getRecords(t, recordsHandler(reg, "proxy."), "")
	want := map[string]string{"proxy.port": "3000", "proxy.scheme": "https"}
	if !reflect.DeepEqual(records[0].Labels, want) {
		t.Errorf("labels = %v, want %v", records[0].Labels, want)
	}

	// The registry's own labels are not modified.
	if len(labels) != 4 {
		t.Errorf("registry labels modified: %v", labels)
	}

	_, records = getRecords(t, recordsHandler(reg, ""), "")
	if records[0].Labels != nil {
		t.Errorf("empty prefix: labels = %v, want none", records[0].Labels)
	}
}

// ---------------------------------------------------------------------------
// apiHandler / requireToken
// ---------------------------------------------------------------------------

func TestAPIHandler_UsesBeaconSettings(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.SetRecord(Record{Host: "web.beacon", Target: "172.17.0.2", Labels: map[string]string{"proxy.port": "3000", "other": "x"}})

	b := &Beacon{APIToken: "secret", RecordLabels: "proxy."}
	code, records := getRecords(t, b.apiHandler(reg), "Bearer secret")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if !reflect.DeepEqual(records[0].Labels, map[string]string{"proxy.port": "3000"}) {
		t.Errorf("labels = %v", records[0].Labels)
	}
}

func TestAPIHandler_TokenProtectsAllPaths(t *testing.T) {
	t.Parallel()

	h := (&Beacon{APIToken: "secret"}).apiHandler(NewRegistry())

	// Checked before routing, known path or not.
	for _, path := range []string{"/api/records", "/api/other"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s without token: status %d, want 401", path, w.Code)
		}
	}
}

func TestRequireToken(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := requireToken("secret", ok)

	cases := map[string]int{
		"Bearer secret": http.StatusOK,
		"":              http.StatusUnauthorized,
		"Bearer wrong":  http.StatusUnauthorized,
		"secret":        http.StatusUnauthorized,
	}

	for auth, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("Authorization %q: status %d, want %d", auth, w.Code, want)
		}
	}
}

func TestRequireToken_EmptyDisablesCheck(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	requireToken("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/records", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status %d, want 200", w.Code)
	}
}

// ---------------------------------------------------------------------------
// apiWarnings
// ---------------------------------------------------------------------------

func TestAPIWarnings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		b    Beacon
		want int
	}{
		{"disabled", Beacon{}, 0},
		{"token without http", Beacon{APIToken: "x"}, 1},
		{"labels without http", Beacon{RecordLabels: "proxy."}, 1},
		{"token and labels without http", Beacon{APIToken: "x", RecordLabels: "proxy."}, 2},
		{"loopback without token", Beacon{APIAddr: "127.0.0.1:8080"}, 0},
		{"public without token", Beacon{APIAddr: "0.0.0.0:8080"}, 1},
		{"public with token", Beacon{APIAddr: "0.0.0.0:8080", APIToken: "x"}, 0},
	}

	for _, c := range cases {
		if got := c.b.apiWarnings(); len(got) != c.want {
			t.Errorf("%s: %d warnings (%v), want %d", c.name, len(got), got, c.want)
		}
	}
}
