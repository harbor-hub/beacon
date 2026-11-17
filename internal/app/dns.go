package app

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// ResolveUpstream normalises upstream server addresses (adds :53 if no port)
// and falls back to /etc/resolv.conf or Google DNS when the list is empty.
func ResolveUpstream(servers []string) []string {
	for i, s := range servers {
		if !strings.Contains(s, ":") {
			servers[i] = s + ":53"
		}
	}

	if len(servers) > 0 {
		return servers
	}

	config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err == nil && len(config.Servers) > 0 {
		seen := make(map[string]bool)

		var result []string

		allLoopback := true
		for _, s := range config.Servers {
			addr := s + ":53"
			if !seen[addr] {
				seen[addr] = true
				result = append(result, addr)
			}
			if ip := net.ParseIP(s); ip == nil || !ip.IsLoopback() {
				allLoopback = false
			}
		}

		// Only append public DNS as fallback when all resolv.conf servers are
		// loopback addresses (e.g. Docker's 127.0.0.11). If the environment
		// already uses public DNS, appending Google's servers causes timeouts
		// in networks that block outbound port 53.
		if allLoopback {
			for _, fb := range []string{"8.8.8.8:53", "8.8.4.4:53"} {
				if !seen[fb] {
					result = append(result, fb)
				}
			}
		}

		return result
	}

	return []string{"8.8.8.8:53", "8.8.4.4:53"}
}

func forward(r *dns.Msg, servers []string) (*dns.Msg, error) {
	c := &dns.Client{Timeout: 2 * time.Second}

	name := ""
	if len(r.Question) > 0 {
		name = r.Question[0].Name
	}

	if LookupEnv("BEACON_DEBUG", false).GetBool() {
		log.Printf("[DEBUG] Forward: %s", name)
	}

	for _, srv := range servers {
		resp, _, err := c.Exchange(r, srv)
		if err == nil {
			return resp, nil
		}
		log.Printf("[DNS] Forward: %s, Upstream %s failed: %v", name, srv, err)
	}

	return nil, fmt.Errorf("all upstream servers failed")
}
