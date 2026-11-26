package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

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
// Listeners and upstream
// ---------------------------------------------------------------------------

func TestParseDNSListeners(t *testing.T) {
	t.Parallel()

	got, err := parseDNSListeners([]string{":5053, :53?upstream=auto", "", "127.0.0.1:54?upstream=1.1.1.1&upstream=[::1]:5353"})
	want := []DNSListener{
		{Addr: ":5053"},
		{Addr: ":53", Upstream: []string{autoUpstream}},
		{Addr: "127.0.0.1:54", Upstream: []string{"1.1.1.1", "[::1]:5353"}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v; want %+v", got, err, want)
	}

	for _, in := range []string{
		"?upstream=auto",   // no address
		":53?stub=self",    // unknown option
		":53?upstream=%zz", // bad escape
	} {
		if _, err := parseDNSListeners([]string{in}); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestValidateDNSListeners(t *testing.T) {
	t.Parallel()

	valid := [][]DNSListener{
		{{Addr: ":53"}},
		{{Addr: ":5053"}, {Addr: ":53", Upstream: []string{autoUpstream}}},
		{{Addr: ":53", Upstream: []string{"1.1.1.1", "8.8.8.8:53", "::1", "[::1]:5353"}}},
	}
	for _, l := range valid {
		if err := validateDNSListeners(l); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}

	invalid := map[string][]DNSListener{
		"none":              nil,
		"duplicate":         {{Addr: ":53"}, {Addr: ":53", Upstream: []string{autoUpstream}}},
		"auto with servers": {{Addr: ":53", Upstream: []string{autoUpstream, "1.1.1.1"}}},
		"hostname":          {{Addr: ":53", Upstream: []string{"dns.google"}}},
	}
	for name, l := range invalid {
		if err := validateDNSListeners(l); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestResolveUpstream(t *testing.T) {
	t.Parallel()

	conf := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(conf, []byte("nameserver 127.0.0.11\noptions ndots:0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveUpstream([]string{autoUpstream}, conf)
	if err != nil || !reflect.DeepEqual(got, []string{"127.0.0.11:53"}) {
		t.Errorf("auto: got %v, %v", got, err)
	}

	got, err = resolveUpstream([]string{"1.1.1.1", "8.8.8.8:5353", "::1"}, conf)
	if want := []string{"1.1.1.1:53", "8.8.8.8:5353", "[::1]:53"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("servers: got %v, %v; want %v", got, err, want)
	}

	empty := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(empty, []byte("options ndots:0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveUpstream([]string{autoUpstream}, empty); err == nil {
		t.Error("auto without nameserver: expected error (no public fallback)")
	}
}

// TestServeDNS_Forward runs a listener with an upstream: its domain is
// answered locally, other names are forwarded over UDP and TCP.
func TestServeDNS_Forward(t *testing.T) {
	t.Parallel()

	upstream := startUpstream(t)

	reg := NewRegistry()
	reg.Set("web.lh", "172.17.0.2")

	addr := freeAddr(t)
	go serveDNS(DNSListener{Addr: addr, Upstream: []string{upstream}}, []string{"lh"}, makeDomainHandler(reg, false))

	for _, n := range []string{"udp", "tcp"} {
		c := &dns.Client{Net: n, Timeout: time.Second}
		for name, want := range map[string]string{"web.lh.": "172.17.0.2", "example.com.": "192.0.2.7"} {
			var resp *dns.Msg
			var err error
			for range 50 { // wait for the listener
				m := new(dns.Msg)
				m.SetQuestion(name, dns.TypeA)
				if resp, _, err = c.Exchange(m, addr); err == nil {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil || len(resp.Answer) != 1 || resp.Answer[0].(*dns.A).A.String() != want {
				t.Errorf("%s %s: got %v, %v; want %s", n, name, resp, err, want)
			}
		}
	}
}

// TestServeDNS_NoUpstream: without an upstream, other names get no answer.
func TestServeDNS_NoUpstream(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)
	go serveDNS(DNSListener{Addr: addr}, []string{"lh"}, makeDomainHandler(NewRegistry(), false))

	c := &dns.Client{Timeout: time.Second}
	var resp *dns.Msg
	var err error
	for range 50 {
		m := new(dns.Msg)
		m.SetQuestion("example.com.", dns.TypeA)
		if resp, _, err = c.Exchange(m, addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || len(resp.Answer) != 0 || resp.Rcode == dns.RcodeSuccess {
		t.Errorf("got %v, %v; want an error rcode", resp, err)
	}
}

func TestForwardHandler_AllFail(t *testing.T) {
	t.Parallel()

	mux := dns.NewServeMux()
	mux.HandleFunc(".", makeForwardHandler([]string{freeAddr(t)}))
	srv, addr := startTestServer(t, mux)
	defer srv.Shutdown() //nolint:errcheck

	if resp := query(t, addr, "example.com.", dns.TypeA); resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("rcode = %s, want SERVFAIL", dns.RcodeToString[resp.Rcode])
	}
}

// startUpstream serves a fake upstream answering 192.0.2.7 for every name,
// over UDP and TCP, and returns its address.
func startUpstream(t *testing.T) string {
	t.Helper()

	h := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		rr, _ := dns.NewRR(r.Question[0].Name + " 60 IN A 192.0.2.7")
		m.Answer = append(m.Answer, rr)
		w.WriteMsg(m)
	})

	addr := freeAddr(t)
	for _, n := range []string{"udp", "tcp"} {
		started := make(chan struct{})
		server := &dns.Server{Addr: addr, Net: n, Handler: h, NotifyStartedFunc: func() { close(started) }}
		go server.ListenAndServe()
		<-started
		t.Cleanup(func() { server.Shutdown() })
	}

	return addr
}

// freeAddr returns a 127.0.0.1 address with a port free for both UDP and TCP.
func freeAddr(t *testing.T) string {
	t.Helper()

	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		if pc, err := net.ListenPacket("udp", addr); err == nil {
			pc.Close()
			return addr
		}
	}
	t.Fatal("no free port")
	return ""
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
