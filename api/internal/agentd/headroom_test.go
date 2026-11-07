package agentd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

func ptr[T any](v T) *T { return &v }

// claimPlatform answers claims 204 and keeps their bodies; a field it does
// not know it refuses as the platform's decoder does (400, unknown field, the
// first in the body's order: json.Marshal sorts a map's keys).
func claimPlatform(t *testing.T, known ...string) (*httptest.Server, func() []map[string]any) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		keys := make([]string, 0, len(b))
		for k := range b {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if !slices.Contains(known, k) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"` + k + `\""}`))
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(bodies)
	}
}

var (
	claimFields    = []string{"host_id", "free_slots", "tools"}
	diskFields     = []string{"disk_free_bytes", "root_free_bytes"}
	headroomFields = []string{"load1", "cpus", "mem_avail_bytes", "mem_total_bytes"}
)

var fixtureHeadroom = &agentd.Headroom{Load1: ptr(1.25), CPUs: 8, MemAvail: ptr(int64(12 << 30)), MemTotal: ptr(int64(32 << 30))}

// R5 phase 2 step 3b: a claim carries the host's headroom, each field it
// read; one not read is left out.
func TestAClaimCarriesTheHostsHeadroom(t *testing.T) {
	srv, bodies := claimPlatform(t, slices.Concat(claimFields, diskFields, headroomFields)...)
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	if cl, err := c.ClaimWithHeadroom(ctx, 1, []agentd.Tool{cursor}, &agentd.Disk{Host: 3 << 30, Root: 2 << 30}, fixtureHeadroom); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	if cl, err := c.ClaimWithHeadroom(ctx, 1, []agentd.Tool{cursor}, nil, &agentd.Headroom{CPUs: 4}); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	got := bodies()
	if b := got[0]; b["load1"] != 1.25 || b["cpus"] != float64(8) || b["mem_avail_bytes"] != float64(12<<30) ||
		b["mem_total_bytes"] != float64(32<<30) || b["disk_free_bytes"] != float64(3<<30) {
		t.Fatalf("the claim's body %v", b)
	}
	if b := got[1]; b["cpus"] != float64(4) || b["load1"] != nil || b["mem_avail_bytes"] != nil || b["mem_total_bytes"] != nil {
		t.Fatalf("a claim with only the cpus read: %v", b)
	}
}

// R5 phase 2 step 3b: a platform from before the headroom refuses it, and
// the claim goes again at once without it, as do the client's later claims,
// with the disk still in them.
func TestAClaimToAPlatformFromBeforeTheHeadroomGoesWithoutIt(t *testing.T) {
	srv, bodies := claimPlatform(t, slices.Concat(claimFields, diskFields)...)
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	disk := &agentd.Disk{Host: 3 << 30, Root: 2 << 30}
	for i := range 2 {
		if cl, err := c.ClaimWithHeadroom(ctx, 1, []agentd.Tool{cursor}, disk, fixtureHeadroom); err != nil || cl != nil {
			t.Fatalf("claim %d: %+v %v", i, cl, err)
		}
	}
	got := bodies()
	if len(got) != 3 || got[0]["cpus"] == nil || got[1]["cpus"] != nil || got[1]["load1"] != nil || got[2]["mem_total_bytes"] != nil {
		t.Fatalf("to a platform from before the headroom: %v", got)
	}
	for i, b := range got {
		if b["disk_free_bytes"] == nil {
			t.Fatalf("claim %d lost the disk: %v", i, b)
		}
	}
}

// A platform from before the disk and the headroom refuses one, then the
// other: the claim still goes, with neither, and so do the later ones.
func TestAClaimToAPlatformFromBeforeTheDiskAndTheHeadroomGoesWithNeither(t *testing.T) {
	srv, bodies := claimPlatform(t, claimFields...)
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	disk := &agentd.Disk{Host: 3 << 30, Root: 2 << 30}
	for i := range 2 {
		if cl, err := c.ClaimWithHeadroom(ctx, 1, []agentd.Tool{cursor}, disk, fixtureHeadroom); err != nil || cl != nil {
			t.Fatalf("claim %d: %+v %v", i, cl, err)
		}
	}
	got := bodies()
	if len(got) != 4 {
		t.Fatalf("%d requests, want 3 for the first claim and 1 for the next: %v", len(got), got)
	}
	for _, b := range got[2:] {
		for _, f := range slices.Concat(diskFields, headroomFields) {
			if b[f] != nil {
				t.Fatalf("a claim after both refusals carries %s: %v", f, b)
			}
		}
	}
}

// A 400 for another reason is the caller's, not a reason to drop the
// headroom and try again.
func TestAClaimRefusedForAnotherReasonIsNotSentAgain(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request","message":"cpus is at least 1"}`))
	}))
	t.Cleanup(srv.Close)
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	if _, err := c.ClaimWithHeadroom(ctx, 1, []agentd.Tool{cursor}, nil, fixtureHeadroom); err == nil || n != 1 {
		t.Fatalf("err %v after %d requests", err, n)
	}
}

func TestParseLoadAndMeminfo(t *testing.T) {
	for in, want := range map[string]float64{"0.52 0.58 0.59 1/123 4567\n": 0.52, "{ 3.10 2.00 1.00 }\n": 3.1} {
		if got := agentd.ParseLoad(in); got == nil || *got != want {
			t.Fatalf("ParseLoad(%q) = %v", in, got)
		}
	}
	if got := agentd.ParseLoad("load"); got != nil {
		t.Fatalf("ParseLoad of no load: %v", *got)
	}
	avail, total := agentd.ParseMeminfo("MemTotal:       32768000 kB\nMemFree:  100 kB\nMemAvailable:   16384000 kB\n")
	if avail == nil || total == nil || *avail != 16384000<<10 || *total != 32768000<<10 {
		t.Fatalf("ParseMeminfo: %v %v", avail, total)
	}
	if avail, total := agentd.ParseMeminfo("MemTotal: 100 kB\n"); avail != nil || total != nil {
		t.Fatal("ParseMeminfo without MemAvailable read something")
	}
	h := agentd.Headroom{Load1: ptr(9.0), CPUs: 8}
	if !h.Overloaded() || (agentd.Headroom{Load1: ptr(8.0), CPUs: 8}).Overloaded() || (agentd.Headroom{CPUs: 8}).Overloaded() {
		t.Fatal("Overloaded is the load above the cores")
	}
	if m := agentd.MeasureHeadroom(nil); m.CPUs < 1 {
		t.Fatalf("MeasureHeadroom: %+v", m)
	}
}

// Elastic slots: a claim carries the host's headroom in its two pairs and
// keeps the slots the answer grants, from a lease's body and a 204's header
// alike; a platform from before both the headroom and the disk refuses one
// field, then the other, and the claim still goes, without them, as do the
// client's later claims.
func TestAClaimCarriesHeadroomAndKeepsItsGrant(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	old, lease := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for _, f := range []string{"cpus", "disk_free_bytes"} {
			if _, sent := b[f]; old && sent {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"` + f + `\""}`))
				return
			}
		}
		if old {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if lease {
			_, _ = w.Write([]byte(`{"task_id":"t_1","attempt_id":"a_1","granted_slots":2}`))
			return
		}
		w.Header().Set(agentd.GrantedSlotsHeader, "3")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	room := &agentd.Headroom{Load1: ptr(1.25), CPUs: 8, MemAvail: ptr(int64(4 << 30)), MemTotal: ptr(int64(16 << 30))}
	disk := &agentd.Disk{Host: 3 << 30, Root: 2 << 30}
	c := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	if _, ok := c.Granted(); ok {
		t.Fatal("granted before any answer")
	}
	if cl, err := c.ClaimHost(ctx, 1, []agentd.Tool{cursor}, disk, room); err != nil || cl != nil {
		t.Fatalf("claim: %+v %v", cl, err)
	}
	if b := bodies[0]; b["load1"] != 1.25 || b["cpus"] != 8.0 || b["mem_avail_bytes"] != float64(4<<30) || b["mem_total_bytes"] != float64(16<<30) {
		t.Fatalf("the claim's body %v", b)
	}
	if n, ok := c.Granted(); !ok || n != 3 {
		t.Fatalf("granted after a 204: %d %v", n, ok)
	}
	lease = true
	if cl, err := c.ClaimHost(ctx, 1, []agentd.Tool{cursor}, disk, &agentd.Headroom{CPUs: 8}); err != nil || cl == nil || *cl.GrantedSlots != 2 {
		t.Fatalf("lease: %+v %v", cl, err)
	}
	if b := bodies[1]; b["load1"] != nil || b["cpus"] != float64(8) || b["mem_total_bytes"] != nil {
		t.Fatalf("readings not taken were sent: %v", b)
	}
	if n, _ := c.Granted(); n != 2 {
		t.Fatalf("granted after a lease: %d", n)
	}

	old = true
	o := &agentd.Client{Base: srv.URL, Host: "h1", Token: "fixture"}
	for i := range 2 {
		if cl, err := o.ClaimHost(ctx, 1, []agentd.Tool{cursor}, disk, room); err != nil || cl != nil {
			t.Fatalf("claim %d to an old platform: %+v %v", i, cl, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	sent := func(b map[string]any) string {
		var s []string
		for _, f := range []string{"cpus", "disk_free_bytes"} {
			if b[f] != nil {
				s = append(s, f)
			}
		}
		return strings.Join(s, ",")
	}
	if len(bodies) != 6 || sent(bodies[2]) != "cpus,disk_free_bytes" || sent(bodies[3]) != "disk_free_bytes" || sent(bodies[4]) != "" || sent(bodies[5]) != "" {
		t.Fatalf("to an old platform: %v", bodies[2:])
	}
	if _, ok := o.Granted(); ok {
		t.Fatal("an old platform's answer granted slots")
	}
}
