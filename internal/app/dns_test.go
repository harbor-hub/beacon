package app

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/miekg/dns"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// startTestServer spins up an in-process UDP DNS server bound to a random
// port on localhost and returns the server and its listen address.
// The caller must call server.Shutdown() when finished.
func startTestServer(t *testing.T, mux *dns.ServeMux) (*dns.Server, string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not open UDP socket: %v", err)
	}
	addr := pc.LocalAddr().String()

	srv := &dns.Server{
		PacketConn: pc,
		Net:        "udp",
		Handler:    mux,
	}

	ready := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(ready) }

	go func() {
		if err := srv.ActivateAndServe(); err != nil {
			// server was shut down – not an error
		}
	}()
	<-ready
	return srv, addr
}

// query sends a single DNS question to addr and returns the response.
func query(t *testing.T, addr, name string, qtype uint16) *dns.Msg {
	t.Helper()
	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	m.RecursionDesired = true
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("DNS exchange to %s failed: %v", addr, err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// ResolveUpstream – table-driven
// ---------------------------------------------------------------------------

func TestResolveUpstream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []string
		wantLen int                              // minimum result length expected (0 = any)
		wantAll []string                         // every element must appear somewhere in result
		check   func(t *testing.T, got []string) // optional extra assertions
	}{
		{
			name:  "already_has_port",
			input: []string{"1.2.3.4:53"},
			check: func(t *testing.T, got []string) {
				if len(got) != 1 || got[0] != "1.2.3.4:53" {
					t.Errorf("unexpected result: %v", got)
				}
			},
		},
		{
			name:  "no_port_gets_53_appended",
			input: []string{"1.2.3.4"},
			check: func(t *testing.T, got []string) {
				if len(got) != 1 || got[0] != "1.2.3.4:53" {
					t.Errorf("expected [1.2.3.4:53], got %v", got)
				}
			},
		},
		{
			name:  "mixed_ports",
			input: []string{"1.1.1.1", "8.8.8.8:5353"},
			check: func(t *testing.T, got []string) {
				if len(got) != 2 {
					t.Fatalf("expected 2 servers, got %d: %v", len(got), got)
				}
				if got[0] != "1.1.1.1:53" {
					t.Errorf("got[0] = %q; want 1.1.1.1:53", got[0])
				}
				if got[1] != "8.8.8.8:5353" {
					t.Errorf("got[1] = %q; want 8.8.8.8:5353", got[1])
				}
			},
		},
		{
			name:  "custom_non_standard_port",
			input: []string{"9.9.9.9:5300"},
			check: func(t *testing.T, got []string) {
				if len(got) != 1 || got[0] != "9.9.9.9:5300" {
					t.Errorf("expected [9.9.9.9:5300], got %v", got)
				}
			},
		},
		{
			name:  "multiple_no_port",
			input: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
			check: func(t *testing.T, got []string) {
				want := []string{"10.0.0.1:53", "10.0.0.2:53", "10.0.0.3:53"}
				if len(got) != len(want) {
					t.Fatalf("expected %v, got %v", want, got)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("got[%d] = %q; want %q", i, got[i], want[i])
					}
				}
			},
		},
		{
			name:  "empty_falls_back_to_resolv_conf_or_google",
			input: []string{},
			check: func(t *testing.T, got []string) {
				if len(got) == 0 {
					t.Error("expected at least one fallback server")
				}
				for _, s := range got {
					if !strings.Contains(s, ":") {
						t.Errorf("fallback server %q missing port", s)
					}
				}
			},
		},
		{
			name:  "nil_falls_back_to_resolv_conf_or_google",
			input: nil,
			check: func(t *testing.T, got []string) {
				if len(got) == 0 {
					t.Error("expected at least one fallback server for nil input")
				}
				for _, s := range got {
					if !strings.Contains(s, ":") {
						t.Errorf("fallback server %q missing port", s)
					}
				}
			},
		},
		{
			name:  "ipv6_with_port",
			input: []string{"[::1]:53"},
			check: func(t *testing.T, got []string) {
				// Already has a colon (two, in fact) – must not be modified
				if len(got) != 1 {
					t.Fatalf("expected 1 server, got %d", len(got))
				}
				// The existing colon detection means "[::1]:53" already contains ":"
				// so it must pass through unchanged.
				if got[0] != "[::1]:53" {
					t.Errorf("got[0] = %q; want [::1]:53", got[0])
				}
			},
		},
		{
			name:  "single_entry_preserved_order",
			input: []string{"4.4.4.4"},
			check: func(t *testing.T, got []string) {
				if got[0] != "4.4.4.4:53" {
					t.Errorf("expected 4.4.4.4:53, got %s", got[0])
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Make a copy so ResolveUpstream's in-place mutation doesn't
			// affect other test cases sharing the slice backing array.
			input := make([]string, len(tc.input))
			copy(input, tc.input)
			got := ResolveUpstream(input)
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestResolveUpstream_ReturnedSliceHasPorts ensures every element in the
// returned slice contains a colon (i.e. a port was appended or was already present)
// for both custom and fallback paths.
func TestResolveUpstream_ReturnedSliceHasPorts(t *testing.T) {
	t.Parallel()
	for _, input := range [][]string{
		{"8.8.8.8"},
		{"1.1.1.1", "9.9.9.9"},
		{},
		nil,
	} {
		input := input
		t.Run("", func(t *testing.T) {
			t.Parallel()
			cp := make([]string, len(input))
			copy(cp, input)
			got := ResolveUpstream(cp)
			for _, s := range got {
				if !strings.Contains(s, ":") {
					t.Errorf("server %q has no port", s)
				}
			}
		})
	}
}

// TestResolveUpstream_EmptyFallbackNonEmpty verifies that when both the caller
// list and /etc/resolv.conf are unavailable, the result is non-empty and all
// entries have ports.
func TestResolveUpstream_EmptyFallbackNonEmpty(t *testing.T) {
	t.Parallel()
	got := ResolveUpstream(nil)
	if len(got) == 0 {
		t.Fatal("fallback must not be empty")
	}
}

// TestResolveUpstream_PublicDNSInResolvConfNoExtraFallback verifies that when
// resolv.conf already has non-loopback (public) servers, Google DNS is NOT
// appended — this prevents timeouts in networks that block outbound port 53.
func TestResolveUpstream_PublicDNSInResolvConfNoExtraFallback(t *testing.T) {
	t.Parallel()
	// Provide a public server directly; ResolveUpstream must return exactly that.
	got := ResolveUpstream([]string{"1.1.1.1"})
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 server (no extra fallback), got %v", got)
	}
	if got[0] != "1.1.1.1:53" {
		t.Errorf("expected 1.1.1.1:53, got %s", got[0])
	}
}

// ---------------------------------------------------------------------------
// DNS handler tests – using an in-process server backed by a real Registry
// ---------------------------------------------------------------------------

func buildHandler(reg *Registry, _ string) dns.HandlerFunc {
	return makeDomainHandler(reg, false)
}

func TestDNSHandler_ARecord_Found(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("web.docker", "172.17.0.2")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "web.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer is not *dns.A")
	}
	if a.A.String() != "172.17.0.2" {
		t.Errorf("expected 172.17.0.2, got %s", a.A.String())
	}
}

func TestDNSHandler_ARecord_NXDOMAIN(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	// intentionally empty – no hosts registered

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "unknown.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected 0 answers on NXDOMAIN, got %d", len(resp.Answer))
	}
}

func TestDNSHandler_AuthoritativeFlagSet(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("api.docker", "172.17.0.5")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "api.docker.", dns.TypeA)
	if !resp.Authoritative {
		t.Error("expected Authoritative=true")
	}
}

func TestDNSHandler_TTL(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("db.docker", "172.17.0.10")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "db.docker.", dns.TypeA)
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	if resp.Answer[0].Header().Ttl != 10 {
		t.Errorf("expected TTL=10, got %d", resp.Answer[0].Header().Ttl)
	}
}

func TestDNSHandler_ClassINET(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("cache.docker", "172.17.0.11")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "cache.docker.", dns.TypeA)
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	if resp.Answer[0].Header().Class != dns.ClassINET {
		t.Errorf("expected ClassINET, got %d", resp.Answer[0].Header().Class)
	}
}

func TestDNSHandler_DeletedEntryReturnsNXDOMAIN(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("ephemeral.docker", "172.17.0.20")
	reg.Delete("ephemeral.docker")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "ephemeral.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN after delete, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestDNSHandler_CaseInsensitiveQuery(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("myapp.docker", "172.17.0.30")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	// The DNS library lowercases names in questions; registry also lowercases.
	resp := query(t, addr, "MYAPP.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR for case-variant query, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestDNSHandler_NonAQueryType_NoAnswer(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("svc.docker", "172.17.0.50")

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	// AAAA query for a host that only has an A record.
	resp := query(t, addr, "svc.docker.", dns.TypeAAAA)
	// Handler only handles TypeA; for other types the answer section is empty
	// and rcode should be NOERROR (no explicit NXDOMAIN set for non-A).
	if len(resp.Answer) != 0 {
		t.Errorf("expected 0 answers for AAAA query, got %d", len(resp.Answer))
	}
}

func TestDNSHandler_MultipleDomains(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("app.dev", "192.168.100.1")
	reg.Set("app.prod", "10.0.0.1")

	mux := dns.NewServeMux()
	for _, domain := range []string{"dev", "prod"} {
		domain := domain
		mux.HandleFunc(domain+".", buildHandler(reg, domain))
	}

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	tests := []struct {
		query  string
		wantIP string
	}{
		{"app.dev.", "192.168.100.1"},
		{"app.prod.", "10.0.0.1"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.query, func(t *testing.T) {
			resp := query(t, addr, tc.query, dns.TypeA)
			if resp.Rcode != dns.RcodeSuccess {
				t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
			}
			if len(resp.Answer) != 1 {
				t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
			}
			a := resp.Answer[0].(*dns.A)
			if a.A.String() != tc.wantIP {
				t.Errorf("expected %s, got %s", tc.wantIP, a.A.String())
			}
		})
	}
}

func TestDNSHandler_OverwrittenIPResolves(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.Set("migrating.docker", "172.17.0.100")
	reg.Set("migrating.docker", "172.17.0.200") // overwrite

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	resp := query(t, addr, "migrating.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	a := resp.Answer[0].(*dns.A)
	if a.A.String() != "172.17.0.200" {
		t.Errorf("expected 172.17.0.200 after overwrite, got %s", a.A.String())
	}
}

// ---------------------------------------------------------------------------
// Concurrent DNS handler stress-test
// ---------------------------------------------------------------------------

func TestDNSHandler_ConcurrentQueries(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	const n = 20
	for i := 0; i < n; i++ {
		reg.Set(fmt.Sprintf("svc%d.docker", i), fmt.Sprintf("10.10.0.%d", i))
	}

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("svc%d.docker.", i)
			resp := query(t, addr, name, dns.TypeA)
			if resp.Rcode != dns.RcodeSuccess {
				t.Errorf("[svc%d] expected NOERROR, got %s", i, dns.RcodeToString[resp.Rcode])
				return
			}
			if len(resp.Answer) != 1 {
				t.Errorf("[svc%d] expected 1 answer, got %d", i, len(resp.Answer))
				return
			}
			a, ok := resp.Answer[0].(*dns.A)
			if !ok {
				t.Errorf("[svc%d] answer not *dns.A", i)
				return
			}
			want := fmt.Sprintf("10.10.0.%d", i)
			if a.A.String() != want {
				t.Errorf("[svc%d] expected %s, got %s", i, want, a.A.String())
			}
		}()
	}
	wg.Wait()
}

func TestDNSHandler_ConcurrentWriteAndQuery(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	mux := dns.NewServeMux()
	mux.HandleFunc("docker.", buildHandler(reg, "docker"))

	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	var wg sync.WaitGroup
	const ops = 30
	wg.Add(ops * 2)

	// Writers register hosts while queries are in flight.
	for i := 0; i < ops; i++ {
		i := i
		go func() {
			defer wg.Done()
			reg.Set(fmt.Sprintf("live%d.docker", i), fmt.Sprintf("10.20.0.%d", i))
		}()
	}
	for i := 0; i < ops; i++ {
		i := i
		go func() {
			defer wg.Done()
			// Result may be NOERROR or NXDOMAIN depending on timing – both are valid.
			_ = query(t, addr, fmt.Sprintf("live%d.docker.", i), dns.TypeA)
		}()
	}
	wg.Wait()
}
