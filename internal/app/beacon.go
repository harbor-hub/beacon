package app

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/miekg/dns"
)

type Beacon struct {
	Version      string
	DNSListeners []DNSListener
	DNSDomains   []string
	Networks     []string
	RecordLabels string
	APIAddr      string
	APIToken     string
	Verbose      bool
}

func New() *Beacon {
	return &Beacon{}
}

func (b *Beacon) Run() error {
	if err := validateDNSListeners(b.DNSListeners); err != nil {
		return err
	}

	dnsListen := make([]string, len(b.DNSListeners))
	for i, l := range b.DNSListeners {
		dnsListen[i] = l.String()
	}

	apiAddr := "(disabled)"
	if b.APIAddr != "" {
		apiAddr = b.APIAddr
	}

	recordLabels := "-"
	if b.RecordLabels != "" {
		recordLabels = b.RecordLabels
	}

	fmt.Printf("\nBeacon %s\n\n  dns-listen:    %s\n  dns-domain:    %s\n  network:       %s\n  record-labels: %s\n  api-listen:    %s\n\n",
		b.Version,
		strings.Join(dnsListen, ", "),
		strings.Join(b.DNSDomains, ", "),
		strings.Join(b.Networks, ", "),
		recordLabels,
		apiAddr,
	)

	for _, w := range b.apiWarnings() {
		log.Printf("[API] Warning: %s", w)
	}

	registry := NewRegistry()
	registry.Verbose = b.Verbose

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	// Docker may emit the start event of Beacon's own container after Beacon
	// is already running, before the subscription below exists. Replaying a
	// few seconds of events covers that; handling an event twice is harmless.
	since := time.Now().Add(-5 * time.Second)
	loadRunning(cli, registry, b.Networks)
	go watchEvents(cli, registry, b.Networks, since)

	domainHandler := makeDomainHandler(registry, b.Verbose)

	errCh := make(chan error, 1+len(b.DNSListeners))

	if b.APIAddr != "" {
		server := newHTTPServer(b.APIAddr, b.httpHandler(registry))
		go func() {
			errCh <- fmt.Errorf("HTTP server error: %w", server.ListenAndServe())
		}()
	}

	for _, l := range b.DNSListeners {
		go func() {
			errCh <- serveDNS(l, b.DNSDomains, domainHandler)
		}()
	}

	return <-errCh
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
