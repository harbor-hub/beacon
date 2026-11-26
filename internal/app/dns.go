package app

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// autoUpstream as a listener's upstream forwards to the nameservers in
// /etc/resolv.conf (in a container on a user-defined network: Docker's DNS).
const autoUpstream = "auto"

const resolvConf = "/etc/resolv.conf"

// DNSListener is one DNS listener.
type DNSListener struct {
	Addr     string
	Upstream []string // where other names go: autoUpstream or IP[:port]; empty: nowhere
}

func (l DNSListener) String() string {
	s := l.Addr
	for i, u := range l.Upstream {
		if i == 0 {
			s += "?"
		} else {
			s += "&"
		}
		s += "upstream=" + u
	}
	return s
}

// parseDNSListeners parses every --dns-listen value; each may hold several
// comma-separated listeners.
func parseDNSListeners(values []string) ([]DNSListener, error) {
	var listeners []DNSListener

	for _, v := range values {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			l, err := parseDNSListen(s)
			if err != nil {
				return nil, err
			}
			listeners = append(listeners, l)
		}
	}

	return listeners, nil
}

// parseDNSListen parses "addr[?upstream=auto|IP[:port]...]": the listen
// address and where names outside the domains are forwarded (none when not
// given).
func parseDNSListen(s string) (DNSListener, error) {
	addr, query, _ := strings.Cut(s, "?")
	l := DNSListener{Addr: addr}
	if addr == "" {
		return DNSListener{}, fmt.Errorf("invalid --dns-listen %q: missing address", s)
	}
	if query == "" {
		return l, nil
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return DNSListener{}, fmt.Errorf("invalid --dns-listen %q: %w", s, err)
	}
	for key, v := range values {
		if key != "upstream" {
			return DNSListener{}, fmt.Errorf("invalid --dns-listen %q: unknown option %q", s, key)
		}
		l.Upstream = v
	}

	return l, nil
}

// validateDNSListeners rejects an empty list, duplicate addresses and invalid
// upstreams.
func validateDNSListeners(listeners []DNSListener) error {
	if len(listeners) == 0 {
		return fmt.Errorf("--dns-listen is required")
	}

	seen := map[string]bool{}
	for _, l := range listeners {
		if seen[l.Addr] {
			return fmt.Errorf("duplicate --dns-listen address %s", l.Addr)
		}
		seen[l.Addr] = true
		if err := validateUpstream(l.Upstream); err != nil {
			return err
		}
	}

	return nil
}

// validateUpstream accepts "auto" alone, or IP[:port] addresses.
func validateUpstream(upstream []string) error {
	for _, u := range upstream {
		if u == autoUpstream {
			if len(upstream) > 1 {
				return fmt.Errorf("invalid --dns-listen upstream: %q cannot be combined with other servers", autoUpstream)
			}
			continue
		}
		host := u
		if h, _, err := net.SplitHostPort(u); err == nil {
			host = h
		}
		if net.ParseIP(host) == nil {
			return fmt.Errorf("invalid --dns-listen upstream %q: must be %q or IP[:port]", u, autoUpstream)
		}
	}
	return nil
}

// resolveUpstream returns host:port addresses; "auto" reads the nameservers
// from the resolv.conf file.
func resolveUpstream(upstream []string, conf string) ([]string, error) {
	if len(upstream) == 1 && upstream[0] == autoUpstream {
		cfg, err := dns.ClientConfigFromFile(conf)
		if err != nil {
			return nil, fmt.Errorf("upstream auto: %w", err)
		}
		if len(cfg.Servers) == 0 {
			return nil, fmt.Errorf("upstream auto: no nameserver in %s", conf)
		}
		upstream = cfg.Servers
	}

	servers := make([]string, len(upstream))
	for i, u := range upstream {
		if _, _, err := net.SplitHostPort(u); err != nil {
			u = net.JoinHostPort(u, "53")
		}
		servers[i] = u
	}
	return servers, nil
}

// serveDNS answers the domains with handler. With an upstream, other names
// are forwarded there, and the listener also serves TCP, which clients use to
// retry a truncated forwarded answer.
func serveDNS(l DNSListener, domains []string, handler dns.HandlerFunc) error {
	mux := dns.NewServeMux()
	for _, d := range domains {
		mux.HandleFunc(d+".", handler)
	}

	nets := []string{"udp"}
	if len(l.Upstream) > 0 {
		servers, err := resolveUpstream(l.Upstream, resolvConf)
		if err != nil {
			return fmt.Errorf("DNS server error: %s: %w", l, err)
		}
		mux.HandleFunc(".", makeForwardHandler(servers))
		nets = append(nets, "tcp")
	}

	errCh := make(chan error, len(nets))
	for _, n := range nets {
		server := &dns.Server{Addr: l.Addr, Net: n, Handler: mux}
		go func() {
			errCh <- fmt.Errorf("DNS server error (%s): %w", n, server.ListenAndServe())
		}()
	}

	return <-errCh
}

// makeForwardHandler relays a query to the servers in order, over the
// client's transport, and returns the first answer unchanged; SERVFAIL when
// none answers.
func makeForwardHandler(servers []string) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		name := ""
		if len(r.Question) > 0 {
			name = r.Question[0].Name
		}

		if LookupEnv("BEACON_DEBUG", false).GetBool() {
			log.Printf("[DEBUG] Forward: %s", name)
		}

		c := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
		if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
			c.Net = "tcp"
		}

		for _, srv := range servers {
			resp, _, err := c.Exchange(r, srv)
			if err != nil {
				log.Printf("[DNS] Forward: %s, upstream %s failed: %v", name, srv, err)
				continue
			}
			w.WriteMsg(resp)
			return
		}

		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(m)
	}
}
