package app

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// NewRegistry
// ---------------------------------------------------------------------------

func TestNewRegistry_NotNil(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
}

func TestNewRegistry_EmptyOnCreation(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	_, ok := r.Get("anything.local")
	if ok {
		t.Error("expected empty registry, but Get returned ok=true")
	}
}

func TestNewRegistry_IndependentInstances(t *testing.T) {
	t.Parallel()
	r1 := NewRegistry()
	r2 := NewRegistry()
	r1.Set("host.local", "1.2.3.4")
	_, ok := r2.Get("host.local")
	if ok {
		t.Error("entries from r1 leaked into r2")
	}
}

// ---------------------------------------------------------------------------
// Set / Get – basic round-trip
// ---------------------------------------------------------------------------

func TestSetGet_BasicRoundTrip(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("myhost.local", "10.0.0.1")
	ip, ok := r.Get("myhost.local")
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if ip != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", ip)
	}
}

// ---------------------------------------------------------------------------
// FQDN normalisation – trailing dot
// ---------------------------------------------------------------------------

func TestSet_AddsFQDNTrailingDot(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("myhost.local", "10.0.0.2") // no trailing dot
	// Get with trailing dot must also find it
	ip, ok := r.Get("myhost.local.")
	if !ok {
		t.Fatal("Get with trailing dot returned ok=false")
	}
	if ip != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", ip)
	}
}

func TestGet_WithoutTrailingDot(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("myhost.local.", "10.0.0.3") // with trailing dot
	ip, ok := r.Get("myhost.local")    // without trailing dot
	if !ok {
		t.Fatal("Get without trailing dot returned ok=false")
	}
	if ip != "10.0.0.3" {
		t.Errorf("expected 10.0.0.3, got %s", ip)
	}
}

func TestSet_TrailingDotIdempotent(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("host.local.", "1.1.1.1")
	r.Set("host.local.", "2.2.2.2") // second write, same FQDN
	ip, ok := r.Get("host.local.")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if ip != "2.2.2.2" {
		t.Errorf("expected 2.2.2.2 after overwrite, got %s", ip)
	}
}

// ---------------------------------------------------------------------------
// Case normalisation
// ---------------------------------------------------------------------------

func TestSet_CaseNormalisation(t *testing.T) {
	t.Parallel()
	cases := []string{
		"MyHost.Local",
		"MYHOST.LOCAL",
		"myhost.local",
		"MyHoSt.LocAl",
	}
	for _, host := range cases {
		host := host
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := NewRegistry()
			r.Set(host, "5.5.5.5")
			ip, ok := r.Get("myhost.local")
			if !ok {
				t.Fatalf("Set(%q) + Get(lower-case) returned ok=false", host)
			}
			if ip != "5.5.5.5" {
				t.Errorf("expected 5.5.5.5, got %s", ip)
			}
		})
	}
}

func TestGet_CaseNormalisation(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("myhost.local", "6.6.6.6")
	queries := []string{
		"MYHOST.LOCAL",
		"MyHost.Local",
		"myhost.local",
	}
	for _, q := range queries {
		q := q
		t.Run(q, func(t *testing.T) {
			t.Parallel()
			ip, ok := r.Get(q)
			if !ok {
				t.Fatalf("Get(%q) returned ok=false", q)
			}
			if ip != "6.6.6.6" {
				t.Errorf("expected 6.6.6.6, got %s", ip)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Overwrite existing key
// ---------------------------------------------------------------------------

func TestSet_OverwriteExistingKey(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("service.internal", "192.168.1.10")
	r.Set("service.internal", "192.168.1.99")
	ip, ok := r.Get("service.internal")
	if !ok {
		t.Fatal("Get returned ok=false after overwrite")
	}
	if ip != "192.168.1.99" {
		t.Errorf("expected 192.168.1.99, got %s", ip)
	}
}

func TestSet_OverwriteWithEmptyIP(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("svc.internal", "10.10.10.10")
	r.Set("svc.internal", "") // overwrite with empty string
	ip, ok := r.Get("svc.internal")
	if !ok {
		t.Fatal("Get returned ok=false; key should still exist after overwrite with empty string")
	}
	if ip != "" {
		t.Errorf("expected empty string after overwrite, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// Multiple entries coexisting
// ---------------------------------------------------------------------------

func TestSet_MultipleEntriesCoexist(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	entries := map[string]string{
		"alpha.test":   "1.0.0.1",
		"beta.test":    "1.0.0.2",
		"gamma.test":   "1.0.0.3",
		"delta.test":   "1.0.0.4",
		"epsilon.test": "1.0.0.5",
	}
	for host, ip := range entries {
		r.Set(host, ip)
	}
	for host, want := range entries {
		got, ok := r.Get(host)
		if !ok {
			t.Errorf("Get(%q) returned ok=false", host)
			continue
		}
		if got != want {
			t.Errorf("Get(%q) = %q; want %q", host, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestDelete_RemovesEntry(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("to-delete.local", "9.9.9.9")
	r.Delete("to-delete.local")
	_, ok := r.Get("to-delete.local")
	if ok {
		t.Error("Get returned ok=true after Delete")
	}
}

func TestDelete_NonExistentKey_NoOp(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// Should not panic
	r.Delete("nonexistent.local")
	_, ok := r.Get("nonexistent.local")
	if ok {
		t.Error("Get returned ok=true for a key that was never set")
	}
}

func TestDelete_CaseNormalisation(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("host.local", "7.7.7.7")
	r.Delete("HOST.LOCAL") // delete using uppercase
	_, ok := r.Get("host.local")
	if ok {
		t.Error("entry should have been deleted via case-normalised key")
	}
}

func TestDelete_FQDNNormalisation(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("host.local.", "8.8.8.8")
	r.Delete("host.local") // no trailing dot
	_, ok := r.Get("host.local.")
	if ok {
		t.Error("entry should have been deleted via FQDN-normalised key")
	}
}

func TestDelete_DoesNotRemoveOtherEntries(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("keep.local", "1.1.1.1")
	r.Set("remove.local", "2.2.2.2")
	r.Delete("remove.local")
	ip, ok := r.Get("keep.local")
	if !ok {
		t.Fatal("Delete removed the wrong entry")
	}
	if ip != "1.1.1.1" {
		t.Errorf("expected 1.1.1.1, got %s", ip)
	}
	_, ok = r.Get("remove.local")
	if ok {
		t.Error("deleted entry still present")
	}
}

func TestDelete_EmptyStringKey_NoOp(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// Should not panic
	r.Delete("")
}

// ---------------------------------------------------------------------------
// Edge cases – empty / whitespace host names
// ---------------------------------------------------------------------------

func TestSet_EmptyHost(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	// An empty host becomes "." after dns.Fqdn – just verify it does not panic
	r.Set("", "1.2.3.4")
	// Retrieving by empty string or "." should work symmetrically
	ip, ok := r.Get("")
	if !ok {
		t.Fatal("Get(\"\") returned ok=false after Set(\"\", ...)")
	}
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", ip)
	}
}

func TestGet_MissingEntry(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	ip, ok := r.Get("nosuchhost.example")
	if ok {
		t.Errorf("expected ok=false for missing entry, got ip=%q", ip)
	}
	if ip != "" {
		t.Errorf("expected empty ip for missing entry, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// Verbose flag (smoke-test: must not panic / deadlock)
// ---------------------------------------------------------------------------

func TestVerbose_SetAndDelete(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Verbose = true
	r.Set("verbose.local", "3.3.3.3")
	r.Delete("verbose.local")
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	const n = 200
	r := NewRegistry()
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			host := fmt.Sprintf("host%d.local", i)
			r.Set(host, fmt.Sprintf("10.0.%d.%d", i/256, i%256))
		}()
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		host := fmt.Sprintf("host%d.local", i)
		_, ok := r.Get(host)
		if !ok {
			t.Errorf("entry %q missing after concurrent writes", host)
		}
	}
}

func TestRegistry_ConcurrentReads(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	const n = 100
	for i := 0; i < n; i++ {
		r.Set(fmt.Sprintf("host%d.local", i), fmt.Sprintf("10.1.%d.%d", i/256, i%256))
	}
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			host := fmt.Sprintf("host%d.local", i)
			_, _ = r.Get(host)
		}()
	}
	wg.Wait()
}

func TestRegistry_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	var wg sync.WaitGroup
	const writers = 50
	const readers = 50
	wg.Add(writers + readers)

	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			r.Set(fmt.Sprintf("rw%d.local", i), fmt.Sprintf("10.2.0.%d", i))
		}()
	}
	for i := 0; i < readers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, _ = r.Get(fmt.Sprintf("rw%d.local", i))
		}()
	}
	wg.Wait()
}

func TestRegistry_ConcurrentDelete(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	const n = 100
	for i := 0; i < n; i++ {
		r.Set(fmt.Sprintf("del%d.local", i), "10.3.0.1")
	}
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			r.Delete(fmt.Sprintf("del%d.local", i))
		}()
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		_, ok := r.Get(fmt.Sprintf("del%d.local", i))
		if ok {
			t.Errorf("entry del%d.local should have been deleted", i)
		}
	}
}

func TestRegistry_ConcurrentOverwrite(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	// All goroutines overwrite the same key; result must be one of the written IPs.
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			r.Set("shared.local", fmt.Sprintf("10.4.0.%d", i))
		}()
	}
	wg.Wait()
	ip, ok := r.Get("shared.local")
	if !ok {
		t.Fatal("key shared.local missing after concurrent overwrites")
	}
	if ip == "" {
		t.Error("ip should not be empty after concurrent overwrites")
	}
}

// ---------------------------------------------------------------------------
// Race-detector stress test (also detected via -race flag)
// ---------------------------------------------------------------------------

func TestRegistry_RaceDetector(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 500; i++ {
			r.Set(fmt.Sprintf("stress%d.local", i%20), fmt.Sprintf("10.5.0.%d", i%256))
		}
		close(done)
	}()

	for i := 0; i < 500; i++ {
		_, _ = r.Get(fmt.Sprintf("stress%d.local", i%20))
	}
	<-done
}

// ---------------------------------------------------------------------------
// SetRecord / List
// ---------------------------------------------------------------------------

func TestSetRecord_StoresAllFields(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.SetRecord(Record{
		Host:      "Web.Local.",
		Target:    "10.0.0.1",
		Container: "web",
		Labels:    map[string]string{"proxy.port": "3000"},
	})

	if ip, ok := r.Get("web.local"); !ok || ip != "10.0.0.1" {
		t.Fatalf("Get = %q, %v", ip, ok)
	}

	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 record, got %d", len(list))
	}
	want := Record{Host: "web.local", Target: "10.0.0.1", Container: "web", Labels: map[string]string{"proxy.port": "3000"}}
	if !reflect.DeepEqual(list[0], want) {
		t.Errorf("record = %+v, want %+v (host lowercase, no trailing dot)", list[0], want)
	}
}

func TestList_SortedByHost(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("c.local", "10.0.0.3")
	r.Set("a.local", "10.0.0.1")
	r.Set("b.local", "10.0.0.2")

	var hosts []string
	for _, rec := range r.List() {
		hosts = append(hosts, rec.Host)
	}
	if !reflect.DeepEqual(hosts, []string{"a.local", "b.local", "c.local"}) {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestList_Empty(t *testing.T) {
	t.Parallel()
	if list := NewRegistry().List(); len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestList_AfterDelete(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Set("a.local", "10.0.0.1")
	r.Set("b.local", "10.0.0.2")
	r.Delete("a.local")

	list := r.List()
	if len(list) != 1 || list[0].Host != "b.local" {
		t.Errorf("list = %+v", list)
	}
}
