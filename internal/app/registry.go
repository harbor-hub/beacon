package app

import (
	"log"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

type Registry struct {
	mu      sync.RWMutex
	records map[string]string // hostname -> IP
	Verbose bool
}

func NewRegistry() *Registry {
	return &Registry{
		records: make(map[string]string),
	}
}

func (r *Registry) Get(host string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ip, ok := r.records[dns.Fqdn(strings.ToLower(host))]

	return ip, ok
}

func (r *Registry) Set(host, ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	host = dns.Fqdn(strings.ToLower(host))
	r.records[host] = ip

	if r.Verbose {
		log.Printf("[DNS] Added: %s -> %s", host, ip)
	}
}

func (r *Registry) Delete(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	host = dns.Fqdn(strings.ToLower(host))
	delete(r.records, host)

	if r.Verbose {
		log.Printf("[DNS] Removed: %s", host)
	}
}
