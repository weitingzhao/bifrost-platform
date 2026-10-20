package approvals

import (
	"context"
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// dirSum is a digest of every file under dir, to show a read wrote nothing.
func dirSum(t *testing.T, dir string) [32]byte {
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

// Lookup (for the agent runtime's controller) reads a record by number or
// id and writes nothing, even when a sweep would expire it.
func TestLookupReadsWithoutSweeping(t *testing.T) {
	dir := t.TempDir()
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) { return nil, nil })
	c := svc.create(context.Background(), "sess-lookup", "cordon_node", "patch node", "uncordon", map[string]any{"name": "node-l"})
	if c.Status != http.StatusCreated || c.Approval.Number == 0 {
		t.Fatalf("create = %d %#v", c.Status, c.Body)
	}
	svc.SetClock(func() time.Time { return time.Now().UTC().Add(30 * 24 * time.Hour) })
	before := dirSum(t, dir)
	for _, ref := range []string{strconv.Itoa(c.Approval.Number), "#" + strconv.Itoa(c.Approval.Number), c.Approval.ID} {
		a, ok, err := svc.Lookup(ref)
		if err != nil || !ok || a.Status != StatusPending {
			t.Fatalf("Lookup(%s) = %s %v %v", ref, a.Status, ok, err)
		}
	}
	if _, ok, err := svc.Lookup("99999"); ok || err != nil {
		t.Fatalf("a missing number = %v %v", ok, err)
	}
	if dirSum(t, dir) != before {
		t.Fatal("Lookup wrote to the store")
	}
	if a, _ := svc.get(c.Approval.ID); a.Status != StatusExpired {
		t.Fatalf("the sweeping read = %s; the case proves nothing unless a sweep would expire it", a.Status)
	}
}

// Waiting (the agent runtime's assessments, TD-391) lists the approvals
// waiting for a decision, oldest first, and writes nothing: one past its
// expiry is left out, not expired, which a sweep would do.
func TestWaitingReadsWithoutSweeping(t *testing.T) {
	dir := t.TempDir()
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) { return nil, nil })
	first := svc.create(context.Background(), "sess-wait", "cordon_node", "patch node", "uncordon", map[string]any{"name": "node-w1"})
	second := svc.create(context.Background(), "sess-wait", "cordon_node", "patch node", "uncordon", map[string]any{"name": "node-w2"})
	if first.Status != http.StatusCreated || second.Status != http.StatusCreated {
		t.Fatalf("create = %d %d", first.Status, second.Status)
	}
	got, err := svc.Waiting()
	if err != nil || len(got) != 2 || got[0].Number != first.Approval.Number || got[1].Number != second.Approval.Number {
		t.Fatalf("Waiting = %v %v", got, err)
	}
	svc.SetClock(func() time.Time { return time.Now().UTC().Add(30 * 24 * time.Hour) })
	before := dirSum(t, dir)
	if got, err = svc.Waiting(); err != nil || len(got) != 0 {
		t.Fatalf("Waiting past their expiry = %v %v", got, err)
	}
	if dirSum(t, dir) != before {
		t.Fatal("Waiting wrote to the store")
	}
	if a, _, _ := svc.Lookup(strconv.Itoa(first.Approval.Number)); a.Status != StatusPending {
		t.Fatalf("the first is %s", a.Status)
	}
}
