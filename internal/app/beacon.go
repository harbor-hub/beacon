package app

import (
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/docker/docker/client"
	"github.com/miekg/dns"
)

type Beacon struct {
	Version  string
	Addr     string
	Domains  []string
	Upstream []string
	Verbose  bool
}

func New() *Beacon {
	return &Beacon{}
}

func (b *Beacon) Run() error {
	fmt.Printf("\nBeacon %s\n\n  addr:     %s\n  domain:   %s\n  upstream: %s\n\n",
		b.Version,
		b.Addr,
		strings.Join(b.Domains, ", "),
		strings.Join(b.Upstream, ", "),
	)

	registry := NewRegistry()
	registry.Verbose = b.Verbose

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	loadRunning(cli, registry)
	go watchEvents(cli, registry)

	for _, domain := range b.Domains {
		dns.HandleFunc(domain+".", makeDomainHandler(registry, b.Verbose))
	}

	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		resp, err := forward(r, b.Upstream)
		if err != nil {
			log.Printf("[DNS] Forward error: %v", err)
			dns.HandleFailed(w, r)
			return
		}

		w.WriteMsg(resp)
	})

	server := &dns.Server{Addr: b.Addr, Net: "udp"}
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("DNS server error: %w", err)
	}

	return nil
}

func makeDomainHandler(registry *Registry, verbose bool) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true

		for _, q := range r.Question {
			if q.Qtype == dns.TypeA {
				if ip, ok := registry.Get(q.Name); ok {
					m.Answer = append(m.Answer, &dns.A{
						Hdr: dns.RR_Header{
							Name:   q.Name,
							Rrtype: dns.TypeA,
							Class:  dns.ClassINET,
							Ttl:    10,
						},
						A: net.ParseIP(ip),
					})
					if verbose {
						log.Printf("[DNS] Query: %s -> %s", q.Name, ip)
					}
				} else {
					if verbose {
						log.Printf("[DNS] Query: %s -> NXDOMAIN", q.Name)
					}
					m.SetRcode(r, dns.RcodeNameError)
				}
			}
		}

		w.WriteMsg(m)
	}
}
