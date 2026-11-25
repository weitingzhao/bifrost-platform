package catalog

import (
	"errors"
	"testing"
	"time"
)

type countingKept struct {
	memKept
	reads int
	fail  bool
}

func (c *countingKept) Read(n string) ([]byte, error) {
	c.reads++
	if c.fail {
		return nil, errors.New("cluster down")
	}
	return c.memKept.Read(n)
}

// 2026-11-24, UI/World: the City's 14 domain reads at once took 16 to 44
// seconds, each a few ConfigMap reads queued behind the cluster client's
// limit. Reads within KeptTTL are answered from memory, a missing name too,
// and this process's writes are seen at once. The control: past KeptTTL the
// store is asked again, and a failed read is not kept.
func TestKeptReadsAreAnsweredFromMemoryForTheTTL(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	under := &countingKept{memKept: memKept{"a": []byte("1")}}
	c := newCachedKept(under, func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if raw, _ := c.Read("a"); string(raw) != "1" {
			t.Fatal(string(raw))
		}
		if raw, _ := c.Read("none"); raw != nil {
			t.Fatal(string(raw))
		}
	}
	if under.reads != 2 {
		t.Fatalf("reads %d, want 2", under.reads)
	}
	_ = c.Write("a", []byte("2"))
	if raw, _ := c.Read("a"); string(raw) != "2" || under.reads != 2 {
		t.Fatalf("own write: %s, reads %d", raw, under.reads)
	}
	now = now.Add(KeptTTL)
	under.fail = true
	if _, err := c.Read("a"); err == nil || under.reads != 3 {
		t.Fatalf("past the TTL the store is asked: err %v reads %d", err, under.reads)
	}
	under.fail = false
	if raw, _ := c.Read("a"); string(raw) != "2" || under.reads != 4 {
		t.Fatalf("a failed read is not kept: %s reads %d", raw, under.reads)
	}
}
