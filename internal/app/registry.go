package app

import (
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// Record is what Beacon knows about a hostname. It is used for DNS answers and
// served as-is by the API.
type Record struct {
	Host      string            `json:"host"` // lowercase, no trailing dot
	Target    string            `json:"target"`
	Container string            `json:"container,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type Registry struct {
	mu      sync.RWMutex
	records map[string]Record // FQDN -> record
	Verbose bool
}

func NewRegistry() *Registry {
	return &Registry{
		records: make(map[string]Record),
	}
}

// Get returns the target IP for host.
func (r *Registry) Get(host string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rec, ok := r.records[fqdn(host)]

	return rec.Target, ok
}

// Set registers host with only a target IP.
func (r *Registry) Set(host, ip string) {
	r.SetRecord(Record{Host: host, Target: ip})
}

// SetRecord registers rec under rec.Host.
func (r *Registry) SetRecord(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fqdn(rec.Host)
	rec.Host = strings.TrimSuffix(key, ".")
	r.records[key] = rec

	if r.Verbose {
		log.Printf("[DNS] Added: %s -> %s", key, rec.Target)
	}
}

func (r *Registry) Delete(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	host = fqdn(host)
	delete(r.records, host)

	if r.Verbose {
		log.Printf("[DNS] Removed: %s", host)
	}
}

// List returns a copy of all records, sorted by host.
func (r *Registry) List() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]Record, 0, len(r.records))
	for _, rec := range r.records {
		list = append(list, rec)
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Host < list[j].Host })

	return list
}

func fqdn(host string) string {
	return dns.Fqdn(strings.ToLower(host))
}
