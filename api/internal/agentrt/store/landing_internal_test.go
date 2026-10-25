package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestLandingNoteVocabulary(t *testing.T) {
	t.Parallel()
	r := map[string]any{"v": 1, "op": "land:1", "kind": "unparked"}
	if err := checkBody("note", map[string]any{"text": "Resume", "landing": r}); err != nil {
		t.Fatal(err)
	}
	r["extra"] = true
	if checkBody("note", map[string]any{"text": "Resume", "landing": r}) == nil {
		t.Fatal("unchecked landing object")
	}
	delete(r, "extra")
	r["v"] = 2
	if checkBody("note", map[string]any{"text": "Resume", "landing": r}) == nil {
		t.Fatal("unknown version")
	}
	if checkBody("note", map[string]any{"landing": r}) == nil {
		t.Fatal("missing human text")
	}
}

// This probe proves Append itself invokes the transition gate after locking;
// it complements, and does not replace, the migrated PostgreSQL test.
type landingAppendProbe struct {
	pgx.Tx
	locked, inserted bool
}

func (p *landingAppendProbe) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (p *landingAppendProbe) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	if strings.Contains(q, "FOR UPDATE") {
		p.locked = true
		return landingHeadRow{}
	}
	p.inserted = true
	return landingHeadRow{insert: true}
}
func (p *landingAppendProbe) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if !p.locked {
		return nil, fmt.Errorf("read before lock")
	}
	return &landingEmptyRows{}, nil
}

type landingHeadRow struct{ insert bool }

func (r landingHeadRow) Scan(dest ...any) error {
	if r.insert {
		return fmt.Errorf("unexpected insert")
	}
	*dest[0].(*int64) = 0
	*dest[1].(*string) = ""
	return nil
}

type landingEmptyRows struct{ pgx.Rows }

func (*landingEmptyRows) Next() bool { return false }
func (*landingEmptyRows) Err() error { return nil }
func (*landingEmptyRows) Close()     {}
func TestLandingAppendEnforcesTransitionAfterLock(t *testing.T) {
	t.Parallel()
	tx := &landingAppendProbe{}
	_, err := Append(context.Background(), tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "Resume", "landing": map[string]any{"v": 1, "kind": "unparked", "op": "land:1"}}})
	if err == nil || !strings.Contains(err.Error(), "illegal") || !tx.locked || tx.inserted {
		t.Fatalf("not refused under lock: err=%v locked=%v inserted=%v", err, tx.locked, tx.inserted)
	}
}
