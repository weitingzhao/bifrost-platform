package questions

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func treeSum(t *testing.T, dir string) [32]byte {
	t.Helper()
	h := sha256.New()
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		_, _ = h.Write([]byte(p))
		_, _ = h.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Lookup (for the agent runtime's controller) reads a question by id or
// number and writes nothing, even when a sweep would expire it.
func TestLookupReadsWithoutSweeping(t *testing.T) {
	s := testSvc(t)
	if res := ask(t, s, "thread-a", "0123456789abcdef", nil); res.Code >= 300 {
		t.Fatalf("ask = %d %v", res.Code, res.Body)
	}
	doc, err := s.load()
	if err != nil || len(doc.Questions) != 1 {
		t.Fatalf("load = %v %v", doc, err)
	}
	q := doc.Questions[0]
	far := time.Now().UTC().Add(60 * 24 * time.Hour)
	s.SetClock(func() time.Time { return far })
	root := filepath.Dir(s.store.path)
	before := treeSum(t, root)
	for _, ref := range []string{q.ID, strconv.Itoa(q.Number)} {
		got, ok, err := s.Lookup(ref)
		if err != nil || !ok || got.ID != q.ID || got.Status != StatusOpen {
			t.Fatalf("Lookup(%s) = %s %s %v %v", ref, got.ID, got.Status, ok, err)
		}
	}
	if _, ok, err := s.Lookup("q_0000000000000000"); ok || err != nil {
		t.Fatalf("a missing question = %v %v", ok, err)
	}
	if treeSum(t, root) != before {
		t.Fatal("Lookup wrote to the store")
	}
	if len(sweep(doc, far)) == 0 {
		t.Fatal("a sweep would not expire it; the case proves nothing")
	}
}
